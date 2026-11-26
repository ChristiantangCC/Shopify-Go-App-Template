// Package crypto encrypts Shopify access and refresh tokens at rest with
// AES-256-GCM, and supports rotating the key.
//
// Ciphertext formats:
//
//	v2:<key id>:<base64(nonce | ciphertext)>   written by Seal
//	v1:<base64(nonce | ciphertext)>            legacy, read only
//
// The key id is the first 8 hex characters of the key's SHA-256, so Open
// knows which key a value needs and IsCurrent can tell values sealed with
// an older key. v1 values carry no id; Open tries every key on them.
//
// Rotating: put the new key in ENCRYPTION_KEY and the old one in
// ENCRYPTION_KEYS_OLD. Everything stays readable, new writes use the new
// key, and the re-encrypt job rewrites old values. See docs/key-rotation.md.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	v1Prefix = "v1:"
	v2Prefix = "v2:"
)

// Cipher seals with the current key and opens with any configured key. It
// is safe for concurrent use.
type Cipher struct {
	currentID string
	// aeads holds every configured key by id, the current one included.
	aeads map[string]cipher.AEAD
	// order is current first, then the old keys as given, for v1 values.
	order []string
}

// New builds a Cipher. current seals everything new; previous keys only
// open values sealed before a rotation. Each key is 64 hex characters (32
// bytes), as produced by: openssl rand -hex 32
func New(current string, previous ...string) (*Cipher, error) {
	c := &Cipher{aeads: map[string]cipher.AEAD{}}
	for i, hexKey := range append([]string{current}, previous...) {
		id, aead, err := parseKey(hexKey)
		if err != nil {
			if i == 0 {
				return nil, fmt.Errorf("crypto: ENCRYPTION_KEY %w", err)
			}
			return nil, fmt.Errorf("crypto: ENCRYPTION_KEYS_OLD entry %d %w", i, err)
		}
		if _, dup := c.aeads[id]; dup {
			return nil, fmt.Errorf("crypto: key %s is configured twice", id)
		}
		c.aeads[id] = aead
		c.order = append(c.order, id)
	}
	c.currentID = c.order[0]
	return c, nil
}

func parseKey(hexKey string) (string, cipher.AEAD, error) {
	key, err := hex.DecodeString(strings.TrimSpace(hexKey))
	if err != nil || len(key) != 32 {
		return "", nil, errors.New("must be 64 hex characters (openssl rand -hex 32)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:4]), aead, nil
}

// CurrentKeyID is the id of the key Seal uses, safe to log.
func (c *Cipher) CurrentKeyID() string { return c.currentID }

// CurrentPrefix is how every value sealed with the current key starts; the
// re-encrypt job selects rows that don't.
func (c *Cipher) CurrentPrefix() string { return v2Prefix + c.currentID + ":" }

// IsCurrent reports whether sealed was written with the current key.
func (c *Cipher) IsCurrent(sealed string) bool {
	return strings.HasPrefix(sealed, c.CurrentPrefix())
}

// Seal encrypts plaintext with the current key.
func (c *Cipher) Seal(plaintext string) (string, error) {
	aead := c.aeads[c.currentID]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("crypto: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return c.CurrentPrefix() + base64.StdEncoding.EncodeToString(sealed), nil
}

// Open decrypts a value produced by Seal with any configured key, or a
// legacy v1 value. A wrong key or tampered value returns an error, never
// garbage.
func (c *Cipher) Open(sealed string) (string, error) {
	if rest, ok := strings.CutPrefix(sealed, v2Prefix); ok {
		id, payload, ok := strings.Cut(rest, ":")
		if !ok {
			return "", errors.New("crypto: malformed ciphertext")
		}
		aead, known := c.aeads[id]
		if !known {
			return "", fmt.Errorf("crypto: sealed with key %s, which is not configured (add it to ENCRYPTION_KEYS_OLD)", id)
		}
		return open(aead, payload)
	}
	if payload, ok := strings.CutPrefix(sealed, v1Prefix); ok {
		// v1 carries no key id: GCM authentication rejects every wrong
		// key, so trying each one is safe.
		for _, id := range c.order {
			if plaintext, err := open(c.aeads[id], payload); err == nil {
				return plaintext, nil
			}
		}
		return "", errors.New("crypto: decryption failed")
	}
	return "", errors.New("crypto: unknown ciphertext format")
}

func open(aead cipher.AEAD, payload string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("crypto: %w", err)
	}
	if len(data) < aead.NonceSize() {
		return "", errors.New("crypto: ciphertext too short")
	}
	nonce, ciphertext := data[:aead.NonceSize()], data[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", errors.New("crypto: decryption failed")
	}
	return string(plaintext), nil
}
