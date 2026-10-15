package exchange

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

var now = time.Unix(1_800_000_000, 0)

// testClient points the exchange client at a fake token endpoint. The
// endpoint URL is https://<shop>/..., so the fake server's host doubles as
// the shop domain.
func testClient(t *testing.T, handler http.HandlerFunc) (*Client, string) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	client := New("client-id", "client-secret")
	client.HTTPClient = server.Client()
	client.now = func() time.Time { return now }
	return client, strings.TrimPrefix(server.URL, "https://")
}

func tokenResponse() map[string]any {
	return map[string]any{
		"access_token":             "shpat_new",
		"scope":                    "write_products",
		"expires_in":               3600,
		"refresh_token":            "refresh_new",
		"refresh_token_expires_in": 7776000,
	}
}

func TestExchangeRequestsAnExpiringToken(t *testing.T) {
	var form url.Values
	client, shop := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		form = r.PostForm
		json.NewEncoder(w).Encode(tokenResponse())
	})

	token, err := client.Exchange(context.Background(), shop, "the-id-token")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	want := map[string]string{
		"grant_type":           "urn:ietf:params:oauth:grant-type:token-exchange",
		"subject_token":        "the-id-token",
		"subject_token_type":   "urn:ietf:params:oauth:token-type:id_token",
		"requested_token_type": "urn:shopify:params:oauth:token-type:offline-access-token",
		"client_id":            "client-id",
		"client_secret":        "client-secret",
		"expiring":             "1",
	}
	for key, value := range want {
		if form.Get(key) != value {
			t.Errorf("form[%s] = %q, want %q", key, form.Get(key), value)
		}
	}
	if token.AccessToken != "shpat_new" || token.RefreshToken != "refresh_new" {
		t.Fatalf("token %+v", token)
	}
	if !token.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("ExpiresAt %v", token.ExpiresAt)
	}
	if !token.RefreshTokenExpiresAt.Equal(now.Add(90 * 24 * time.Hour)) {
		t.Fatalf("RefreshTokenExpiresAt %v", token.RefreshTokenExpiresAt)
	}
}

func TestRefreshUsesTheRefreshGrant(t *testing.T) {
	var form url.Values
	client, shop := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		form = r.PostForm
		json.NewEncoder(w).Encode(tokenResponse())
	})

	if _, err := client.Refresh(context.Background(), shop, "refresh_old"); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "refresh_old" {
		t.Fatalf("form %v", form)
	}
}

func TestRefreshMapsA401ToErrInvalidRefreshToken(t *testing.T) {
	client, shop := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_request"}`))
	})
	_, err := client.Refresh(context.Background(), shop, "stale")
	if err != ErrInvalidRefreshToken {
		t.Fatalf("err = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestExchangeRejectsAnEmptyResponse(t *testing.T) {
	client, shop := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	})
	if _, err := client.Exchange(context.Background(), shop, "tok"); err == nil {
		t.Fatal("expected an error")
	}
}
