package crypto

import (
	"strings"
	"testing"
)

const testKey = "71f32b865b94a3618868b73f42f4bc84f5c0d04e14f84b3f9d4a16771dcf9a05"

func TestSealRoundTrips(t *testing.T) {
	c, err := New(testKey)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sealed, err := c.Seal("shpat_secret_token")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if strings.Contains(sealed, "shpat") {
		t.Fatalf("ciphertext leaks plaintext: %q", sealed)
	}
	if !strings.HasPrefix(sealed, "v1:") {
		t.Fatalf("missing version prefix: %q", sealed)
	}
	opened, err := c.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened != "shpat_secret_token" {
		t.Fatalf("got %q, want the original plaintext", opened)
	}
}

func TestSealUsesAFreshNoncePerCall(t *testing.T) {
	c, _ := New(testKey)
	a, _ := c.Seal("same")
	b, _ := c.Seal("same")
	if a == b {
		t.Fatal("two seals of the same plaintext must differ")
	}
}

func TestOpenRejects(t *testing.T) {
	c, _ := New(testKey)
	sealed, _ := c.Seal("value")

	other, _ := New("0000000000000000000000000000000000000000000000000000000000000001")

	cases := map[string]struct {
		cipher *Cipher
		input  string
	}{
		"wrong key":      {other, sealed},
		"tampered":       {c, sealed[:len(sealed)-2] + "AA"},
		"unknown format": {c, "v2:" + sealed[3:]},
		"not base64":     {c, "v1:%%%"},
		"too short":      {c, "v1:AAAA"},
		"empty":          {c, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tc.cipher.Open(tc.input); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestNewRejectsBadKeys(t *testing.T) {
	for _, key := range []string{"", "abc", strings.Repeat("g", 64), testKey + "00"} {
		if _, err := New(key); err == nil {
			t.Fatalf("New(%q) accepted a bad key", key)
		}
	}
}
