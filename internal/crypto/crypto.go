// Package crypto encrypts Shopify access and refresh tokens at rest with
// AES-256-GCM. The ciphertext carries a version prefix so the format can
// change later without breaking stored rows.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const v1Prefix = "v1:"

// Cipher seals and opens short secrets. It is safe for concurrent use.
type Cipher struct {
	aead cipher.AEAD
}

// New builds a Cipher from a 64-hex-character key (32 bytes), as produced
// by: openssl rand -hex 32
func New(hexKey string) (*Cipher, error) {
	key, err := hex.DecodeString(strings.TrimSpace(hexKey))
	if err != nil || len(key) != 32 {
		return nil, errors.New("crypto: ENCRYPTION_KEY must be 64 hex characters (openssl rand -hex 32)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Seal encrypts plaintext into "v1:" + base64(nonce | ciphertext).
func (c *Cipher) Seal(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("crypto: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return v1Prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Open decrypts a value produced by Seal. A wrong key or tampered value
// returns an error, never garbage.
func (c *Cipher) Open(sealed string) (string, error) {
	raw, ok := strings.CutPrefix(sealed, v1Prefix)
	if !ok {
		return "", errors.New("crypto: unknown ciphertext format")
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("crypto: %w", err)
	}
	if len(data) < c.aead.NonceSize() {
		return "", errors.New("crypto: ciphertext too short")
	}
	nonce, ciphertext := data[:c.aead.NonceSize()], data[c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", errors.New("crypto: decryption failed")
	}
	return string(plaintext), nil
}
