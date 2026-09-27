package pgpii

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
)

func keyring(t *testing.T) *crypto.Keyring {
	t.Helper()
	kr, err := crypto.NewKeyring("k1", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)), nil)
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

func TestSealOpenRoundTrip(t *testing.T) {
	p := NewPassports(keyring(t))
	id := uuid.New()
	s, err := p.Seal(Customers, id, " a1234 5678 ")
	if err != nil {
		t.Fatal(err)
	}
	if s.Enc == "" || strings.Contains(s.Enc, "5678") || s.Last4 != "5678" {
		t.Fatalf("sealed = %+v", s)
	}
	got, err := p.Open(Customers, id, s.Enc, "")
	if err != nil || got != "A12345678" {
		t.Fatalf("open = %q, %v", got, err)
	}
	if _, err := p.Open(Customers, uuid.New(), s.Enc, ""); err == nil {
		t.Fatal("ciphertext must be bound to its row")
	}
	if _, err := p.Open(BookingParticipants, id, s.Enc, ""); err == nil {
		t.Fatal("ciphertext must be bound to its table")
	}
}

func TestBlindIndexDedupesNormalizedInput(t *testing.T) {
	p := NewPassports(keyring(t))
	a, _ := p.Seal(Customers, uuid.New(), "a1234 5678")
	b, _ := p.Seal(BookingParticipants, uuid.New(), "A12345678")
	if a.Hash == "" || a.Hash != b.Hash || a.Hash != p.Hash(" A1234 5678") {
		t.Fatalf("hashes differ: %q %q", a.Hash, b.Hash)
	}
	if a.Enc == b.Enc {
		t.Fatal("ciphertexts must be randomized")
	}
	if p.Hash("A12345679") == a.Hash {
		t.Fatal("different passports share a hash")
	}
}

func TestBlankAndLegacy(t *testing.T) {
	p := NewPassports(keyring(t))
	s, err := p.Seal(Customers, uuid.New(), "  ")
	if err != nil || s != (Sealed{}) {
		t.Fatalf("blank = %+v, %v", s, err)
	}
	if p.Hash("") != "" {
		t.Fatal("blank hash must never match")
	}
	got, err := p.Open(Customers, uuid.New(), "", "LEGACY1")
	if err != nil || got != "LEGACY1" {
		t.Fatalf("legacy fallback = %q, %v", got, err)
	}
}
