// Package webhooks receives, verifies and handles Shopify webhooks. The
// registry in registry.go is the single place a topic is wired up; verify.go
// authenticates deliveries and subscribe.go keeps shop-specific (dynamic)
// subscriptions in sync via the Admin API.
package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// VerifyHMAC reports whether the X-Shopify-Hmac-Sha256 header matches the
// raw request body: base64 of HMAC-SHA256 with the app's client secret.
// It must run on the raw bytes, before any JSON parsing.
func VerifyHMAC(secret string, body []byte, headerValue string) bool {
	given, err := base64.StdEncoding.DecodeString(headerValue)
	if err != nil || len(given) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(given, mac.Sum(nil))
}
