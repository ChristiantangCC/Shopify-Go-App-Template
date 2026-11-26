package crypto

import (
	"encoding/base64"
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
	if !strings.HasPrefix(sealed, "v2:"+c.CurrentKeyID()+":") {
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
		"unknown format": {c, "v9:" + sealed[3:]},
		"v2 without id":  {c, "v2:AAAA"},
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

const oldKey = "0000000000000000000000000000000000000000000000000000000000000001"

// sealV1 writes the legacy format (no key id), as stored before rotation
// support existed.
func sealV1(t *testing.T, hexKey, plaintext string) string {
	t.Helper()
	_, aead, err := parseKey(hexKey)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	sealed := aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return v1Prefix + base64.StdEncoding.EncodeToString(sealed)
}

func TestRotation(t *testing.T) {
	before, _ := New(oldKey)
	sealedBefore, _ := before.Seal("token")

	after, err := New(testKey, oldKey)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := after.Open(sealedBefore); err != nil || got != "token" {
		t.Fatalf("value sealed with the old key: %q err %v", got, err)
	}
	if after.IsCurrent(sealedBefore) {
		t.Fatal("a value sealed with the old key is not current")
	}
	resealed, _ := after.Seal("token")
	if !after.IsCurrent(resealed) {
		t.Fatal("Seal must use the current key")
	}

	// Once the old key is dropped, its values fail loudly, naming the key.
	dropped, _ := New(testKey)
	if _, err := dropped.Open(sealedBefore); err == nil || !strings.Contains(err.Error(), before.CurrentKeyID()) {
		t.Fatalf("missing old key must be reported by id, got %v", err)
	}
}

func TestOpensLegacyV1WithAnyKey(t *testing.T) {
	c, _ := New(testKey, oldKey)
	for _, key := range []string{testKey, oldKey} {
		if got, err := c.Open(sealV1(t, key, "legacy")); err != nil || got != "legacy" {
			t.Fatalf("v1 sealed with %s...: %q err %v", key[:4], got, err)
		}
	}
	if c.IsCurrent(sealV1(t, testKey, "legacy")) {
		t.Fatal("v1 values are never current: they get re-encrypted into v2")
	}
}

func TestNewRejectsADuplicateKey(t *testing.T) {
	if _, err := New(testKey, testKey); err == nil {
		t.Fatal("the current key repeated as an old key must be rejected")
	}
	if _, err := New(testKey, "nope"); err == nil {
		t.Fatal("a malformed old key must be rejected")
	}
}
