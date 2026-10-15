package sessiontoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const (
	clientID     = "client-id-123"
	clientSecret = "shhh-secret"
)

var issued = time.Unix(1_800_000_000, 0)

func token(t *testing.T, secret string, header map[string]any, claims map[string]any) string {
	t.Helper()
	rawHeader, _ := json.Marshal(header)
	rawClaims, _ := json.Marshal(claims)
	signing := base64.RawURLEncoding.EncodeToString(rawHeader) + "." + base64.RawURLEncoding.EncodeToString(rawClaims)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validClaims() map[string]any {
	return map[string]any{
		"iss":  "https://example-shop.myshopify.com/admin",
		"dest": "https://example-shop.myshopify.com",
		"aud":  clientID,
		"sub":  "9001",
		"exp":  issued.Add(time.Minute).Unix(),
		"nbf":  issued.Unix(),
	}
}

func hs256Header() map[string]any { return map[string]any{"alg": "HS256", "typ": "JWT"} }

func TestVerifyAcceptsShopifysToken(t *testing.T) {
	claims, err := Verify(token(t, clientSecret, hs256Header(), validClaims()), clientID, clientSecret, issued)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Shop() != "example-shop.myshopify.com" {
		t.Fatalf("shop = %q", claims.Shop())
	}
}

func TestVerifyRejects(t *testing.T) {
	alter := func(change func(map[string]any)) string {
		claims := validClaims()
		change(claims)
		return token(t, clientSecret, hs256Header(), claims)
	}
	cases := map[string]string{
		"wrong secret":  token(t, "other-secret", hs256Header(), validClaims()),
		"other app":     alter(func(c map[string]any) { c["aud"] = "someone-else" }),
		"expired":       alter(func(c map[string]any) { c["exp"] = issued.Add(-time.Minute).Unix() }),
		"not yet valid": alter(func(c map[string]any) { c["nbf"] = issued.Add(time.Hour).Unix() }),
		"not a shop":    alter(func(c map[string]any) { c["dest"] = "https://evil.example.com" }),
		"iss mismatch":  alter(func(c map[string]any) { c["iss"] = "https://other-shop.myshopify.com/admin" }),
		"alg none":      token(t, clientSecret, map[string]any{"alg": "none", "typ": "HS256"}, validClaims()),
		"garbage":       "not.a.token",
		"empty":         "",
	}
	for name, tk := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Verify(tk, clientID, clientSecret, issued); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestVerifyParsesTheAlgorithmInsteadOfMatchingText(t *testing.T) {
	// The header claims alg none but mentions HS256 in another field; a
	// substring check would be fooled.
	crafted := token(t, clientSecret, map[string]any{"alg": "none", "kid": "HS256"}, validClaims())
	if _, err := Verify(crafted, clientID, clientSecret, issued); err == nil {
		t.Fatal("crafted alg-none token accepted")
	}
}

func TestMiddleware(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var got Session
	handler := Middleware(clientID, clientSecret, logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = SessionFrom(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	// Middleware uses time.Now, so sign a token that is valid now.
	claims := validClaims()
	claims["exp"] = time.Now().Add(time.Minute).Unix()
	claims["nbf"] = time.Now().Add(-time.Minute).Unix()
	fresh := token(t, clientSecret, hs256Header(), claims)

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"valid", "Bearer " + fresh, http.StatusNoContent},
		{"no scheme", fresh, http.StatusUnauthorized},
		{"bad token", "Bearer nope", http.StatusUnauthorized},
		{"missing", "", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/admin/x", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("code %d, want %d", rec.Code, tt.want)
			}
			if tt.want == http.StatusNoContent && (got.Shop != "example-shop.myshopify.com" || got.Token != fresh) {
				t.Fatalf("session %+v", got)
			}
		})
	}
}
