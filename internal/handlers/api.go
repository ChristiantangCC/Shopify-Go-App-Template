package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/apikey"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/admin"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/sessiontoken"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/tokens"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/webhooks"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
)

// API is the app's JSON API: the embedded admin UI's routes under
// /api/admin (session-token auth) and the optional server-to-server routes
// under /api/service (API keys). Every operation is declared with huma, so
// its request and response types are the OpenAPI spec: `bun run
// codegen:api` writes api/openapi.yaml from it and the frontend's typed
// client (web/frontend/src/api/schema.ts) from that.
//
// Webhooks, Events, the App Proxy, health probes and the SPA stay plain
// chi handlers: they aren't part of the client contract.
type API struct {
	Tokens     *tokens.Service
	River      *river.Client[pgx.Tx]
	Store      *store.Store
	Admin      *admin.Client
	Subscriber *webhooks.Subscriber
	// Host is the app's public base URL, the webhook callback syncs use.
	Host string

	ClientID     string
	ClientSecret string

	// BillingEnabled mounts the billing routes; AppHandle builds the
	// managed-pricing plans URL.
	BillingEnabled bool
	AppHandle      string

	// APIKeys mounts /api/service when non-nil.
	APIKeys *apikey.Checker

	Logger *slog.Logger
}

// Every slice this API returns is non-nil (make, never a nil var), so
// arrays are documented as never null and the generated TypeScript types
// are plain T[] instead of T[] | null. huma reads this while building
// schemas, so it is set before any operation registers.
func init() {
	huma.DefaultArrayNullable = false
}

// apiTitle and apiVersion head the OpenAPI document.
const (
	apiTitle   = "Shopify Go App Template API"
	apiVersion = "1.0.0"
)

// Mount registers every JSON operation on router and returns the huma API
// (its OpenAPI() is the spec). The spec is not served: it is generated
// into the repo instead (cmd/openapi).
func (h API) Mount(router chi.Router) huma.API {
	config := huma.DefaultConfig(apiTitle, apiVersion)
	config.OpenAPIPath = ""
	config.DocsPath = ""
	config.SchemasPath = ""
	// No $schema links in response bodies.
	config.CreateHooks = nil
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"sessionToken": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
			Description:  "App Bridge session token: `await shopify.idToken()`.",
		},
		"apiKey": {
			Type:        "http",
			Scheme:      "bearer",
			Description: "A server-to-server key from API_KEY or API_KEYS.",
		},
	}
	api := humachi.New(router, config)

	adminAPI := huma.NewGroup(api, "/api/admin")
	adminAPI.UseSimpleModifier(func(op *huma.Operation) {
		op.Tags = []string{"admin"}
		op.Security = []map[string][]string{{"sessionToken": {}}}
		documentUnauthorized(op)
	})
	adminAPI.UseMiddleware(h.requireSession(api))
	h.registerSession(adminAPI)
	h.registerProducts(adminAPI)
	h.registerWebhookSubscriptions(adminAPI)
	h.registerSettings(adminAPI)
	h.registerBulk(adminAPI)
	if h.BillingEnabled {
		h.registerBilling(adminAPI)
	}

	if h.APIKeys != nil {
		serviceAPI := huma.NewGroup(api, "/api/service")
		serviceAPI.UseSimpleModifier(func(op *huma.Operation) {
			op.Tags = []string{"service"}
			op.Security = []map[string][]string{{"apiKey": {}}}
			documentUnauthorized(op)
		})
		serviceAPI.UseMiddleware(h.requireAPIKey(api))
		h.registerServiceProducts(serviceAPI)
	}
	return api
}

// documentUnauthorized adds the 401 every authenticated operation can
// answer. Group modifiers run after huma has built an operation's
// responses from its Errors, so the response is added directly.
func documentUnauthorized(op *huma.Operation) {
	if op.Responses == nil {
		op.Responses = map[string]*huma.Response{}
	}
	op.Responses["401"] = &huma.Response{
		Description: "Unauthorized: missing or invalid credentials, or the shop needs a new token exchange",
		Content: map[string]*huma.MediaType{
			"application/problem+json": {Schema: &huma.Schema{Ref: "#/components/schemas/ErrorModel"}},
		},
	}
}

// requireSession rejects requests without a valid App Bridge session
// token, with 401; the frontend reacts by fetching a fresh token and
// retrying once. The shop always comes from the verified token.
func (h API) requireSession(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		session, err := sessiontoken.FromHeader(ctx.Header("Authorization"), h.ClientID, h.ClientSecret, time.Now())
		if err != nil {
			h.Logger.Warn("[SessionToken] Rejected request", "path", ctx.URL().Path, "error", err)
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(huma.WithContext(ctx, sessiontoken.WithSession(ctx.Context(), session)))
	}
}

// requireAPIKey rejects server-to-server requests without a configured key.
func (h API) requireAPIKey(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		caller, ok := h.APIKeys.Check(ctx.Header("Authorization"))
		if !ok {
			h.Logger.Warn("[APIKey] Rejected request", "path", ctx.URL().Path)
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "unauthorized")
			return
		}
		h.Logger.Info("[APIKey] Call", "caller", caller, "method", ctx.Method(), "path", ctx.URL().Path)
		next(huma.WithContext(ctx, apikey.WithCaller(ctx.Context(), caller)))
	}
}

// sessionFrom returns the verified session requireSession placed in ctx.
func sessionFrom(ctx context.Context) (sessiontoken.Session, error) {
	session, ok := sessiontoken.SessionFrom(ctx)
	if !ok {
		return sessiontoken.Session{}, huma.Error401Unauthorized("unauthorized")
	}
	return session, nil
}

// adminError maps errors from the token and Admin API layers to API
// errors. A needs-reauth becomes 401: the frontend reacts by re-running
// the session bootstrap with a fresh id_token, which performs a new token
// exchange.
func adminError(logger *slog.Logger, err error) error {
	var gqlErrs admin.GraphQLErrors
	switch {
	case errors.Is(err, tokens.ErrNeedsReauth):
		return huma.Error401Unauthorized("reauthorize")
	case errors.As(err, &gqlErrs):
		logger.Error("[API] GraphQL error", "error", err)
		return huma.Error502BadGateway("shopify rejected the request")
	default:
		logger.Error("[API] Shopify call failed", "error", err)
		return huma.Error502BadGateway("shopify unreachable")
	}
}
