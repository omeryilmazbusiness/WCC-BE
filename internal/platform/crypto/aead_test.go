package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func newKey(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func TestRoundTripAndAAD(t *testing.T) {
	k, err := NewKeyring("k1", newKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	ct, err := k.Encrypt("A1234567", "customers.passport:1")
	if err != nil {
		t.Fatal(err)
	}
	pt, err := k.Decrypt(ct, "customers.passport:1")
	if err != nil || pt != "A1234567" {
		t.Fatalf("round trip failed: %q %v", pt, err)
	}
	if _, err := k.Decrypt(ct, "customers.passport:2"); err == nil {
		t.Fatal("ciphertext must be bound to its AAD")
	}
	ct2, _ := k.Encrypt("A1234567", "customers.passport:1")
	if ct == ct2 {
		t.Fatal("nonce must randomize ciphertexts")
	}
}

func TestRotation(t *testing.T) {
	oldKey := newKey(t)
	old, _ := NewKeyring("k1", oldKey, nil)
	ct, _ := old.Encrypt("secret", "")

	rotated, err := NewKeyring("k2", newKey(t), []string{"k1:" + oldKey})
	if err != nil {
		t.Fatal(err)
	}
	if pt, err := rotated.Decrypt(ct, ""); err != nil || pt != "secret" {
		t.Fatalf("old ciphertext must stay readable: %v", err)
	}
	if !rotated.NeedsRotation(ct) {
		t.Fatal("old ciphertext should be flagged for rotation")
	}
	fresh, _ := rotated.Encrypt("secret", "")
	if rotated.NeedsRotation(fresh) {
		t.Fatal("fresh ciphertext uses active key")
	}
}

func TestRejectsBadInput(t *testing.T) {
	if _, err := NewKeyring("k1", "short", nil); err == nil {
		t.Fatal("expected invalid key error")
	}
	k, _ := NewKeyring("k1", newKey(t), nil)
	for _, bad := range []string{"", "plain", "v1:k9:AAAA", "v1:k1:!!"} {
		if _, err := k.Decrypt(bad, ""); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestBlindIndexNormalizes(t *testing.T) {
	k, _ := NewKeyring("k1", newKey(t), nil)
	if k.BlindIndex(" a123 ") != k.BlindIndex("A123") {
		t.Fatal("blind index should normalize case and whitespace")
	}
	if k.BlindIndex("A123") == k.BlindIndex("A124") {
		t.Fatal("different values must differ")
	}
}
