package crypto

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSecretBoxRoundTripAndBinding(t *testing.T) {
	k, _ := NewKeyring("k1", newKey(t), nil)
	box := NewSecretBox(k)
	b := Binding{Table: "ai_settings", RowID: uuid.New(), BranchID: uuid.New()}

	sealed, err := box.Seal(map[string]string{"api_key": "sk-123"}, b)
	if err != nil || !strings.HasPrefix(sealed, k.ActivePrefix()) || strings.Contains(sealed, "sk-123") {
		t.Fatalf("sealed=%q err=%v", sealed, err)
	}
	got, err := box.Open(sealed, b)
	if err != nil || got["api_key"] != "sk-123" {
		t.Fatalf("open: %v %v", got, err)
	}
	for _, other := range []Binding{
		{Table: "external_integrations", RowID: b.RowID, BranchID: b.BranchID},
		{Table: b.Table, RowID: uuid.New(), BranchID: b.BranchID},
		{Table: b.Table, RowID: b.RowID, BranchID: uuid.New()},
	} {
		if _, err := box.Open(sealed, other); err == nil {
			t.Fatalf("ciphertext opened under foreign binding %+v", other)
		}
	}
}

func TestSecretBoxEmptyAndUnbound(t *testing.T) {
	k, _ := NewKeyring("k1", newKey(t), nil)
	box := NewSecretBox(k)
	b := Binding{Table: "t", RowID: uuid.New()}
	if s, err := box.Seal(nil, b); err != nil || s != "" {
		t.Fatalf("empty bag: %q %v", s, err)
	}
	if m, err := box.Open("", b); err != nil || len(m) != 0 {
		t.Fatalf("empty open: %v %v", m, err)
	}
	if _, err := box.Seal(map[string]string{"k": "v"}, Binding{Table: "t"}); err == nil {
		t.Fatal("seal without row id must fail")
	}
}

func TestBlindIndexKeySurvivesRotation(t *testing.T) {
	blind := newKey(t)
	oldKey := newKey(t)
	k1, _ := NewKeyring("k1", oldKey, nil)
	k2, _ := NewKeyring("k2", newKey(t), []string{"k1:" + oldKey})
	if k1.BlindIndex("A1") == k2.BlindIndex("A1") {
		t.Fatal("derived blind keys follow the active key")
	}
	if err := k1.UseBlindIndexKey(blind); err != nil {
		t.Fatal(err)
	}
	if err := k2.UseBlindIndexKey(blind); err != nil {
		t.Fatal(err)
	}
	if k1.BlindIndex("A1") != k2.BlindIndex("A1") {
		t.Fatal("a pinned blind key must survive rotation")
	}
	if k1.UseBlindIndexKey("short") == nil {
		t.Fatal("invalid blind key must be rejected")
	}
	if k2.ActivePrefix() != "v1:k2:" {
		t.Fatalf("prefix=%q", k2.ActivePrefix())
	}
}
