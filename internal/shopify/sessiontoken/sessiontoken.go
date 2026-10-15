// Package sessiontoken verifies Shopify session tokens (the id_token App
// Bridge mints for every embedded request) and guards the /api/admin routes
// with them. The token is the only proof of who is calling; the shop always
// comes from a verified token's dest claim, never from a query parameter.
package sessiontoken

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/shopdomain"
)

// Claims are the parts of a Shopify session token this app checks.
type Claims struct {
	Iss  string `json:"iss"`
	Dest string `json:"dest"`
	Aud  string `json:"aud"`
	Sub  string `json:"sub"`
	Exp  int64  `json:"exp"`
	Nbf  int64  `json:"nbf"`
}

// Shop is the myshopify domain from the token's dest (https://<shop>).
func (c Claims) Shop() string {
	return strings.TrimPrefix(c.Dest, "https://")
}

// Session is what a verified request knows about its caller.
type Session struct {
	// Shop is the verified myshopify domain.
	Shop string
	// Token is the raw session token, needed again for token exchange.
	Token  string
	Claims Claims
}

// leeway tolerates small clock differences between Shopify and the box.
const leeway = 10 * time.Second

// Verify checks a Shopify session token: HS256 signed with the app's client
// secret, issued for this app (aud = client id), valid now, and destined for
// a real myshopify domain.
func Verify(token, clientID, clientSecret string, now time.Time) (Claims, error) {
	var claims Claims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, errors.New("session token: malformed")
	}
	rawHeader, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return claims, errors.New("session token: bad header")
	}
	var header struct {
		Alg string `json:"alg"`
	}
	// The algorithm is parsed, not substring-matched, so a crafted header
	// like {"alg":"none","typ":"HS256"} cannot slip through.
	if json.Unmarshal(rawHeader, &header) != nil || header.Alg != "HS256" {
		return claims, errors.New("session token: unexpected algorithm")
	}
	mac := hmac.New(sha256.New, []byte(clientSecret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return claims, errors.New("session token: bad signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(payload, &claims) != nil {
		return claims, errors.New("session token: bad payload")
	}
	switch {
	case claims.Aud != clientID:
		return claims, errors.New("session token: issued for another app")
	case now.After(time.Unix(claims.Exp, 0).Add(leeway)):
		return claims, errors.New("session token: expired")
	case now.Before(time.Unix(claims.Nbf, 0).Add(-leeway)):
		return claims, errors.New("session token: not valid yet")
	case !shopdomain.Valid(claims.Shop()):
		return claims, errors.New("session token: no shop")
	case !strings.HasPrefix(claims.Iss, claims.Dest):
		return claims, errors.New("session token: iss and dest disagree")
	}
	return claims, nil
}

type sessionKey struct{}

// SessionFrom returns the verified session placed in ctx by Middleware.
func SessionFrom(ctx context.Context) (Session, bool) {
	session, ok := ctx.Value(sessionKey{}).(Session)
	return session, ok
}

// Middleware rejects requests without a valid `Authorization: Bearer
// <id_token>` session token, with 401. The frontend reacts to a 401 by
// fetching a fresh token from App Bridge and retrying once.
func Middleware(clientID, clientSecret string, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			token = strings.TrimSpace(token)
			claims, err := Verify(token, clientID, clientSecret, time.Now())
			if err != nil {
				logger.Warn("[SessionToken] Rejected request", "path", r.URL.Path, "error", err)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			session := Session{Shop: claims.Shop(), Token: token, Claims: claims}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, session)))
		})
	}
}
