package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/jobs"
)

// SessionInfo is what the session bootstrap tells the frontend.
type SessionInfo struct {
	Shop           string `json:"shop" doc:"The shop's myshopify domain."`
	IsNew          bool   `json:"isNew" doc:"True when this open installed (or reinstalled) the app."`
	BillingEnabled bool   `json:"billingEnabled" doc:"Whether the billing pages are available."`
}

type sessionOutput struct {
	Body SessionInfo
}

// registerSession adds POST /api/admin/session: the frontend calls it on
// every app open. It runs a token exchange only when the shop has no
// usable offline token, and queues a webhook-subscription sync so dynamic
// topics follow the registry (and the callback URL follows a changed dev
// tunnel).
func (h API) registerSession(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "bootstrapSession",
		Method:      http.MethodPost,
		Path:        "/session",
		Summary:     "Bootstrap the shop's session on app open",
		Errors:      []int{http.StatusBadGateway},
	}, func(ctx context.Context, _ *struct{}) (*sessionOutput, error) {
		session, err := sessionFrom(ctx)
		if err != nil {
			return nil, err
		}
		isNew, err := h.Tokens.EnsureInstall(ctx, session.Shop, session.Token)
		if err != nil {
			h.Logger.Error("[Session] Install failed", "shop", session.Shop, "error", err)
			return nil, huma.Error502BadGateway("token exchange failed")
		}
		if isNew {
			h.Logger.Info("[Session] App installed", "shop", session.Shop)
		}
		// OncePerWindow: repeated app opens share one sync per shop and host.
		if _, err := h.River.Insert(ctx, jobs.SyncWebhooksArgs{Shop: session.Shop, Host: h.Host}, jobs.OncePerWindow()); err != nil {
			// The app still works without the sync; the next open retries it.
			h.Logger.Warn("[Session] Could not queue webhook sync", "shop", session.Shop, "error", err)
		}
		return &sessionOutput{Body: SessionInfo{Shop: session.Shop, IsNew: isNew, BillingEnabled: h.BillingEnabled}}, nil
	})
}
