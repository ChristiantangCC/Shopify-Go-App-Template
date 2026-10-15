package tokens_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/crypto"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/exchange"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/tokens"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/store"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/testdb"
)

const testKey = "71f32b865b94a3618868b73f42f4bc84f5c0d04e14f84b3f9d4a16771dcf9a05"

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

// fixture wires a tokens.Service against a fake Shopify token endpoint.
// The endpoint's host doubles as the shop domain, like in the exchange
// tests. handler decides what the endpoint answers; calls counts hits.
func fixture(t *testing.T, handler http.HandlerFunc) (service *tokens.Service, shops *store.Store, shop string, calls *atomic.Int32) {
	t.Helper()
	calls = &atomic.Int32{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	cipher, err := crypto.New(testKey)
	if err != nil {
		t.Fatalf("crypto: %v", err)
	}
	shops = store.New(testdb.Pool(t), cipher)
	ex := exchange.New("client-id", "client-secret")
	ex.HTTPClient = server.Client()
	service = tokens.New(shops, ex, discard)
	return service, shops, strings.TrimPrefix(server.URL, "https://"), calls
}

func grant(accessToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":             accessToken,
			"scope":                    "write_products",
			"expires_in":               3600,
			"refresh_token":            "refresh_rotated",
			"refresh_token_expires_in": 7776000,
		})
	}
}

func seedInstall(t *testing.T, shops *store.Store, shop string, expiresIn time.Duration) {
	t.Helper()
	_, _, err := shops.SaveInstall(context.Background(), shop, exchange.Token{
		AccessToken:           "shpat_old",
		ExpiresAt:             time.Now().Add(expiresIn),
		RefreshToken:          "refresh_old",
		RefreshTokenExpiresAt: time.Now().Add(90 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestAccessTokenRefreshesOnceUnderConcurrency(t *testing.T) {
	service, shops, shop, calls := fixture(t, grant("shpat_new"))
	// One minute left is inside the five-minute refresh margin.
	seedInstall(t, shops, shop, time.Minute)

	const callers = 10
	results := make([]string, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = service.AccessToken(context.Background(), shop)
		}()
	}
	wg.Wait()

	for i := range callers {
		if errs[i] != nil || results[i] != "shpat_new" {
			t.Fatalf("caller %d: token %q err %v", i, results[i], errs[i])
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want exactly 1", calls.Load())
	}
}

func TestAccessTokenSkipsTheNetworkWhileFresh(t *testing.T) {
	service, shops, shop, calls := fixture(t, grant("shpat_new"))
	seedInstall(t, shops, shop, time.Hour)

	token, err := service.AccessToken(context.Background(), shop)
	if err != nil || token != "shpat_old" {
		t.Fatalf("token %q err %v", token, err)
	}
	if calls.Load() != 0 {
		t.Fatalf("a fresh token must not hit the network, got %d calls", calls.Load())
	}
}

func TestAccessTokenFlagsReauthWhenShopifyRejectsTheRefreshToken(t *testing.T) {
	service, shops, shop, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_request"}`))
	})
	seedInstall(t, shops, shop, time.Minute)

	if _, err := service.AccessToken(context.Background(), shop); !errors.Is(err, tokens.ErrNeedsReauth) {
		t.Fatalf("err = %v, want ErrNeedsReauth", err)
	}
	install, err := shops.GetInstall(context.Background(), shop)
	if err != nil || !install.NeedsReauth {
		t.Fatalf("install %+v err %v, want NeedsReauth", install, err)
	}
	// Later calls stay terminal without more network traffic.
	if _, err := service.AccessToken(context.Background(), shop); !errors.Is(err, tokens.ErrNeedsReauth) {
		t.Fatalf("second call: %v, want ErrNeedsReauth", err)
	}
}

func TestForceRefreshReusesARotationThatAlreadyHappened(t *testing.T) {
	service, shops, shop, calls := fixture(t, grant("shpat_new"))
	seedInstall(t, shops, shop, time.Hour)

	// The caller failed with a token that is no longer the stored one, so
	// no refresh call is needed.
	token, err := service.ForceRefresh(context.Background(), shop, "shpat_some_older_token")
	if err != nil || token != "shpat_old" {
		t.Fatalf("token %q err %v", token, err)
	}
	if calls.Load() != 0 {
		t.Fatalf("calls = %d, want 0", calls.Load())
	}

	// The caller failed with the stored token itself: now it must rotate.
	token, err = service.ForceRefresh(context.Background(), shop, "shpat_old")
	if err != nil || token != "shpat_new" || calls.Load() != 1 {
		t.Fatalf("token %q err %v calls %d", token, err, calls.Load())
	}
}

func TestEnsureInstallLeavesAHealthyInstallAlone(t *testing.T) {
	service, shops, shop, calls := fixture(t, grant("shpat_new"))
	seedInstall(t, shops, shop, time.Hour)

	isNew, err := service.EnsureInstall(context.Background(), shop, "unused-session-token")
	if err != nil || isNew {
		t.Fatalf("isNew %v err %v", isNew, err)
	}
	if calls.Load() != 0 {
		t.Fatalf("a healthy install must not re-exchange, got %d calls", calls.Load())
	}
}

func TestEnsureInstallExchangesForANewShop(t *testing.T) {
	service, shops, shop, calls := fixture(t, grant("shpat_first"))

	isNew, err := service.EnsureInstall(context.Background(), shop, "the-id-token")
	if err != nil || !isNew || calls.Load() != 1 {
		t.Fatalf("isNew %v err %v calls %d", isNew, err, calls.Load())
	}
	install, err := shops.GetInstall(context.Background(), shop)
	if err != nil || install.AccessToken != "shpat_first" {
		t.Fatalf("install %+v err %v", install, err)
	}
}
