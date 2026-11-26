package handlers_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/apikey"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/crypto"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/handlers"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/exchange"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/testdb"
)

const (
	apiClientID     = "client-id"
	apiClientSecret = "client-secret"
	apiShop         = "example-shop.myshopify.com"
	apiKey          = "0123456789abcdef0123456789abcdef-service"
)

// sessionToken mints an App Bridge session token valid now.
func sessionToken(t *testing.T) string {
	t.Helper()
	header, _ := json.Marshal(map[string]any{"alg": "HS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{
		"iss": "https://" + apiShop + "/admin", "dest": "https://" + apiShop, "aud": apiClientID,
		"sub": "1", "exp": time.Now().Add(time.Minute).Unix(), "nbf": time.Now().Add(-time.Minute).Unix(),
	})
	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	mac := hmac.New(sha256.New, []byte(apiClientSecret))
	mac.Write([]byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func apiRouter(t *testing.T) http.Handler {
	t.Helper()
	database := testdb.Open(t)
	cipher, _ := crypto.New("71f32b865b94a3618868b73f42f4bc84f5c0d04e14f84b3f9d4a16771dcf9a05")
	riverClient, err := river.NewClient(riverpgxv5.New(database.Pool), &river.Config{})
	if err != nil {
		t.Fatal(err)
	}
	shops := store.New(database.Gorm, cipher, nil)
	// Settings rows reference the shop's install, which the session
	// bootstrap creates in the real flow.
	if _, _, err := shops.SaveInstall(context.Background(), apiShop, exchange.Token{AccessToken: "shpat_x"}); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	handlers.API{
		Store:        shops,
		River:        riverClient,
		Host:         "https://app.example.com",
		ClientID:     apiClientID,
		ClientSecret: apiClientSecret,
		APIKeys:      apikey.NewChecker([]apikey.Key{{Name: "test", Value: apiKey}}),
		Logger:       slog.New(slog.DiscardHandler),
	}.Mount(router)
	return router
}

func call(t *testing.T, router http.Handler, method, path, auth, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var decoded map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
	return rec.Code, decoded
}

func TestAPIRejectsMissingCredentials(t *testing.T) {
	router := apiRouter(t)
	for _, path := range []string{"/api/admin/settings", "/api/service/products?shop=" + apiShop} {
		code, body := call(t, router, http.MethodGet, path, "", "")
		if code != http.StatusUnauthorized || body["status"] != float64(401) {
			t.Errorf("%s without credentials: %d %v", path, code, body)
		}
	}
	// A session token is not an API key, and the other way round.
	if code, _ := call(t, router, http.MethodGet, "/api/service/products?shop="+apiShop, sessionToken(t), ""); code != http.StatusUnauthorized {
		t.Errorf("session token on /api/service: %d", code)
	}
	if code, _ := call(t, router, http.MethodGet, "/api/admin/settings", apiKey, ""); code != http.StatusUnauthorized {
		t.Errorf("API key on /api/admin: %d", code)
	}
}

func TestSettingsRoundTripThroughTheAPI(t *testing.T) {
	router := apiRouter(t)
	token := sessionToken(t)

	code, body := call(t, router, http.MethodGet, "/api/admin/settings", token, "")
	if code != http.StatusOK || body["syncEnabled"] != true || body["notificationEmail"] != "" {
		t.Fatalf("defaults: %d %v", code, body)
	}

	code, body = call(t, router, http.MethodPut, "/api/admin/settings", token, `{"notificationEmail":"nope","syncEnabled":false}`)
	if code != http.StatusUnprocessableEntity || body["detail"] != "notificationEmail must be an email address" {
		t.Fatalf("invalid email: %d %v", code, body)
	}

	// The schema rejects what the type doesn't declare, before the handler.
	code, _ = call(t, router, http.MethodPut, "/api/admin/settings", token, `{"notificationEmail":"","syncEnabled":false,"admin":true}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown field: %d", code)
	}
	code, _ = call(t, router, http.MethodPut, "/api/admin/settings", token, `{"notificationEmail":""}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("missing field: %d", code)
	}

	code, body = call(t, router, http.MethodPut, "/api/admin/settings", token, `{"notificationEmail":" ops@example.com ","syncEnabled":false}`)
	if code != http.StatusOK || body["notificationEmail"] != "ops@example.com" {
		t.Fatalf("save: %d %v", code, body)
	}
	code, body = call(t, router, http.MethodGet, "/api/admin/settings", token, "")
	if code != http.StatusOK || body["notificationEmail"] != "ops@example.com" || body["syncEnabled"] != false {
		t.Fatalf("after save: %d %v", code, body)
	}
}

func TestServiceProductsValidatesTheShop(t *testing.T) {
	router := apiRouter(t)
	code, body := call(t, router, http.MethodGet, "/api/service/products?shop=evil.example.com", apiKey, "")
	if code != http.StatusBadRequest {
		t.Fatalf("non-myshopify shop: %d %v", code, body)
	}
	if code, _ := call(t, router, http.MethodGet, "/api/service/products", apiKey, ""); code != http.StatusUnprocessableEntity {
		t.Fatalf("missing shop: %d", code)
	}
}
