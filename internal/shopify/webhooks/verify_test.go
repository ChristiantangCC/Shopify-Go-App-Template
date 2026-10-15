package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestVerifyHMACAcceptsShopifysSignature(t *testing.T) {
	body := []byte(`{"id":9001,"topic":"products/update"}`)
	if !VerifyHMAC("secret", body, sign("secret", body)) {
		t.Fatal("valid signature rejected")
	}
}

func TestVerifyHMACRejects(t *testing.T) {
	body := []byte(`{"id":9001}`)
	cases := map[string]struct {
		body   []byte
		header string
	}{
		"wrong secret":  {body, sign("other", body)},
		"tampered body": {[]byte(`{"id":9002}`), sign("secret", body)},
		"not base64":    {body, "%%%"},
		"empty header":  {body, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if VerifyHMAC("secret", tc.body, tc.header) {
				t.Fatal("invalid signature accepted")
			}
		})
	}
}
