package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// BlindIndexer computes keyed equality hashes for encrypted columns.
type BlindIndexer interface {
	BlindIndex(value string) string
}

// FieldCipher encrypts searchable columns (e.g. passports).
type FieldCipher interface {
	Cipher
	BlindIndexer
}

// Rotator tells re-encryption jobs which ciphertexts are sealed with a
// retired key.
type Rotator interface {
	NeedsRotation(ciphertext string) bool
	// ActivePrefix is the leading text of every ciphertext sealed with the
	// active key, usable in SQL filters (LIKE prefix || '%').
	ActivePrefix() string
}

// ActivePrefix returns "v1:<activeKeyID>:".
func (k *Keyring) ActivePrefix() string {
	return prefix + ":" + k.activeID + ":"
}

// UseBlindIndexKey pins blind indexes to a dedicated base64 32-byte key so
// they survive rotation of the encryption key. Without it the index key is
// derived from the active encryption key. Call before the keyring is shared.
func (k *Keyring) UseBlindIndexKey(keyB64 string) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(keyB64))
	if err != nil {
		return fmt.Errorf("crypto: blind index key not base64: %w", err)
	}
	if len(raw) != 32 {
		return fmt.Errorf("crypto: blind index key must be 32 bytes, got %d", len(raw))
	}
	mac := hmac.New(sha256.New, raw)
	mac.Write([]byte("wodi-blind-index-v1"))
	k.blindKey = mac.Sum(nil)
	return nil
}
