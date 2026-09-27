// Package crypto provides authenticated encryption for secrets and PII at rest.
// Ciphertexts are versioned "v1:<keyID>:<base64(nonce|sealed)>" so keys can
// rotate while old rows stay readable.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const prefix = "v1"

var (
	ErrMalformed  = errors.New("crypto: malformed ciphertext")
	ErrUnknownKey = errors.New("crypto: unknown key id")
)

// Cipher encrypts and decrypts short values. Implementations must be safe
// for concurrent use.
type Cipher interface {
	Encrypt(plaintext string, aad string) (string, error)
	Decrypt(ciphertext string, aad string) (string, error)
}

// Keyring is an AES-256-GCM Cipher with one active key and older keys kept
// for decryption. AAD binds a ciphertext to its column/row context.
type Keyring struct {
	activeID string
	aeads    map[string]cipher.AEAD
	blindKey []byte
}

// NewKeyring builds a keyring from base64 32-byte keys. previous entries are
// "id:base64key".
func NewKeyring(activeID, activeKeyB64 string, previous []string) (*Keyring, error) {
	if activeID == "" || strings.Contains(activeID, ":") {
		return nil, fmt.Errorf("crypto: invalid key id %q", activeID)
	}
	k := &Keyring{activeID: activeID, aeads: map[string]cipher.AEAD{}}
	raw, err := k.add(activeID, activeKeyB64)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, raw)
	mac.Write([]byte("wodi-blind-index-v1"))
	k.blindKey = mac.Sum(nil)
	for _, p := range previous {
		id, key, ok := strings.Cut(p, ":")
		if !ok {
			return nil, fmt.Errorf("crypto: previous key must be id:base64")
		}
		if _, err := k.add(id, key); err != nil {
			return nil, err
		}
	}
	return k, nil
}

func (k *Keyring) add(id, keyB64 string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(keyB64))
	if err != nil {
		return nil, fmt.Errorf("crypto: key %s not base64: %w", id, err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("crypto: key %s must be 32 bytes, got %d", id, len(raw))
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	k.aeads[id] = gcm
	return raw, nil
}

func (k *Keyring) Encrypt(plaintext, aad string) (string, error) {
	gcm := k.aeads[k.activeID]
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), []byte(aad))
	return prefix + ":" + k.activeID + ":" + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (k *Keyring) Decrypt(ciphertext, aad string) (string, error) {
	parts := strings.SplitN(ciphertext, ":", 3)
	if len(parts) != 3 || parts[0] != prefix {
		return "", ErrMalformed
	}
	gcm, ok := k.aeads[parts[1]]
	if !ok {
		return "", ErrUnknownKey
	}
	raw, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(raw) < gcm.NonceSize() {
		return "", ErrMalformed
	}
	nonce, sealed := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	out, err := gcm.Open(nil, nonce, sealed, []byte(aad))
	if err != nil {
		return "", ErrMalformed
	}
	return string(out), nil
}

// NeedsRotation reports whether a ciphertext was sealed with a non-active key.
func (k *Keyring) NeedsRotation(ciphertext string) bool {
	parts := strings.SplitN(ciphertext, ":", 3)
	return len(parts) == 3 && parts[1] != k.activeID
}

// BlindIndex is a keyed deterministic hash for equality lookups on encrypted
// values (e.g. passport dedupe) without storing plaintext.
func (k *Keyring) BlindIndex(value string) string {
	mac := hmac.New(sha256.New, k.blindKey)
	mac.Write([]byte(strings.ToUpper(strings.TrimSpace(value))))
	return hex.EncodeToString(mac.Sum(nil))
}
