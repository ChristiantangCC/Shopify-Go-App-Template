// Package appproxy verifies requests that Shopify forwards through the app's
// App Proxy (https://<store>/apps/<subpath>/... → this app).
//
// Storefront code is public, so the storefront cannot hold a secret. Instead
// Shopify signs every forwarded request: it adds shop, logged_in_customer_id,
// path_prefix and timestamp to the query string, and a `signature` that is the
// HMAC-SHA256 of those parameters with the app's client secret. Only Shopify
// and this app know the secret.
package appproxy

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/shopdomain"
)

// Caller is what a verified App Proxy request tells about who is calling.
type Caller struct {
	Shop string
	// CustomerID is the logged-in storefront customer's numeric id, "" when anonymous.
	CustomerID string
}

type callerKey struct{}

// CallerFrom returns the verified caller placed in ctx by Verify.
func CallerFrom(ctx context.Context) (Caller, bool) {
	caller, ok := ctx.Value(callerKey{}).(Caller)
	return caller, ok
}

// Signature computes the App Proxy signature of a query string: every
// parameter except `signature`, sorted by key, as "key=value" (multiple values
// joined with ","), concatenated without separator, HMAC-SHA256 with the
// secret, hex encoded.
func Signature(query url.Values, secret string) string {
	keys := make([]string, 0, len(query))
	for key := range query {
		if key != "signature" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var message strings.Builder
	for _, key := range keys {
		message.WriteString(key)
		message.WriteString("=")
		message.WriteString(strings.Join(query[key], ","))
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message.String()))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyOptions configures Verify.
type VerifyOptions struct {
	Secret string
	// MaxAge is how old the signed timestamp may be (replay protection).
	MaxAge time.Duration
	Logger *slog.Logger
	// CheckShop, when set, decides whether the signed shop is one this app
	// serves, typically by looking the install up in the database. A nil
	// CheckShop accepts every validly signed shop.
	CheckShop func(ctx context.Context, shop string) error
	// Now is overridable in tests.
	Now func() time.Time
}

// Verify is middleware that lets a request through only when it carries a
// valid, fresh App Proxy signature from an installed shop. Anything else gets
// 401 without detail.
func Verify(opts VerifyOptions) func(http.Handler) http.Handler {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			reject := func(reason string) {
				opts.Logger.Warn("[AppProxy] Rejected request", "reason", reason, "path", r.URL.Path)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
			}

			given, err := hex.DecodeString(query.Get("signature"))
			if err != nil || len(given) == 0 {
				reject("missing or malformed signature")
				return
			}
			expected, _ := hex.DecodeString(Signature(query, opts.Secret))
			if !hmac.Equal(given, expected) {
				reject("bad signature")
				return
			}
			shop := query.Get("shop")
			if !shopdomain.Valid(shop) {
				reject("not a myshopify domain")
				return
			}
			// The timestamp is checked before CheckShop: a replayed request
			// is rejected without touching the database.
			timestamp, err := strconv.ParseInt(query.Get("timestamp"), 10, 64)
			if err != nil {
				reject("bad timestamp")
				return
			}
			if age := now().Sub(time.Unix(timestamp, 0)); age > opts.MaxAge || age < -opts.MaxAge {
				reject("stale timestamp")
				return
			}
			if opts.CheckShop != nil {
				if err := opts.CheckShop(r.Context(), shop); err != nil {
					reject("unknown shop")
					return
				}
			}

			caller := Caller{Shop: shop, CustomerID: query.Get("logged_in_customer_id")}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, caller)))
		})
	}
}

// RateLimitKey returns a key func that rate limits on the logged-in
// customer, otherwise the visitor's IP.
//
// The IP comes from X-Forwarded-For, which the visitor can pre-fill with
// anything, so the leftmost entry is never trusted. Each proxy in front of
// the app appends the address it received the request from; trustedHops is
// how many of those trusted proxies there are, and the visitor's real IP is
// the entry that many places from the right. Behind Shopify's App Proxy and
// one reverse proxy of your own (Traefik, nginx, a load balancer) that is 2:
// Shopify appends the visitor, your proxy appends Shopify. With no reverse
// proxy of your own it is 1. When the header is shorter than expected, the
// TCP peer is used.
func RateLimitKey(trustedHops int) func(r *http.Request) (string, error) {
	return func(r *http.Request) (string, error) {
		if caller, ok := CallerFrom(r.Context()); ok && caller.CustomerID != "" {
			return "customer:" + caller.CustomerID, nil
		}
		if ip := forwardedIP(r.Header.Values("X-Forwarded-For"), trustedHops); ip != "" {
			return "ip:" + ip, nil
		}
		// RemoteAddr is "ip:port", and the port differs per connection.
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			return "ip:" + host, nil
		}
		return "ip:" + r.RemoteAddr, nil
	}
}

// forwardedIP picks the entry trustedHops places from the right of the
// combined X-Forwarded-For values, or "" when there is no such entry.
func forwardedIP(headers []string, trustedHops int) string {
	if trustedHops < 1 {
		return ""
	}
	var entries []string
	for _, header := range headers {
		for _, entry := range strings.Split(header, ",") {
			if entry = strings.TrimSpace(entry); entry != "" {
				entries = append(entries, entry)
			}
		}
	}
	if len(entries) < trustedHops {
		return ""
	}
	return entries[len(entries)-trustedHops]
}
