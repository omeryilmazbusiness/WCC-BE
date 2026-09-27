package secretcfg_test

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/app/secretcfg"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
)

func vault(t *testing.T) secretcfg.Vault {
	t.Helper()
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	k, err := crypto.NewKeyring("k1", base64.StdEncoding.EncodeToString(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	return secretcfg.NewVault(crypto.NewSecretBox(k))
}

func TestApplySealLoad(t *testing.T) {
	v := vault(t)
	b := crypto.Binding{Table: "external_integrations", RowID: uuid.New(), BranchID: uuid.New()}

	cfg, err := v.Apply(secretcfg.Config{}, json.RawMessage(`{"tenant":"acme","api_key":"sk-1234567"}`))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := v.Seal(cfg, b)
	if err != nil || strings.Contains(string(cfg.Public), "sk-") {
		t.Fatalf("public=%s err=%v", cfg.Public, err)
	}

	loaded, err := v.Load(cfg.Public, sealed, b)
	if err != nil || loaded.Secrets["api_key"] != "sk-1234567" || loaded.Hints()["api_key"] != "••••4567" {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}

	// Echoing the redacted config keeps the credential; "" clears it.
	kept, _ := v.Apply(loaded, json.RawMessage(`{"tenant":"acme2"}`))
	if kept.Secrets["api_key"] != "sk-1234567" || !strings.Contains(string(kept.Public), "acme2") {
		t.Fatalf("kept=%+v", kept)
	}
	cleared, _ := v.Apply(loaded, json.RawMessage(`{"api_key":""}`))
	if _, ok := cleared.Secrets["api_key"]; ok {
		t.Fatal("empty value must clear the secret")
	}
	var merged map[string]string
	_ = json.Unmarshal(loaded.Merged(), &merged)
	if merged["api_key"] != "sk-1234567" || merged["tenant"] != "acme" {
		t.Fatalf("merged=%v", merged)
	}
}

func TestLoadFallsBackToLegacyPlaintext(t *testing.T) {
	v := vault(t)
	b := crypto.Binding{Table: "file_sync_connections", RowID: uuid.New(), BranchID: uuid.New()}
	loaded, err := v.Load(json.RawMessage(`{"drive":"d1","client_secret":"legacy-secret"}`), "", b)
	if err != nil || loaded.Secrets["client_secret"] != "legacy-secret" || strings.Contains(string(loaded.Public), "legacy") {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	sealed, _ := v.Seal(secretcfg.Config{Secrets: map[string]string{"client_secret": "new"}}, b)
	loaded, _ = v.Load(json.RawMessage(`{"client_secret":"legacy-secret"}`), sealed, b)
	if loaded.Secrets["client_secret"] != "new" {
		t.Fatal("sealed value must win over legacy plaintext")
	}
	if _, err := v.Load(nil, sealed, crypto.Binding{Table: b.Table, RowID: uuid.New(), BranchID: b.BranchID}); err == nil {
		t.Fatal("sealed bag must not open for another row")
	}
}
