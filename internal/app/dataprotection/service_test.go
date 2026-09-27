package dataprotection_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/app/dataprotection"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
)

func key(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }

func keyring(t *testing.T, active string, previous ...string) *crypto.Keyring {
	t.Helper()
	keys := map[string]string{"k0": key(1), "k1": key(2)}
	var prev []string
	for _, id := range previous {
		prev = append(prev, id+":"+keys[id])
	}
	kr, err := crypto.NewKeyring(active, keys[active], prev)
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

type memSecrets struct {
	rows  map[string][]dataprotection.SecretRow
	saves int
}

func (m *memSecrets) PendingSecrets(_ context.Context, t dataprotection.SecretTable, after uuid.UUID, limit int, _ string) ([]dataprotection.SecretRow, error) {
	var out []dataprotection.SecretRow
	for _, r := range m.rows[t.Name] {
		if bytes.Compare(r.ID[:], after[:]) > 0 && len(out) < limit {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memSecrets) SaveSecrets(_ context.Context, t dataprotection.SecretTable, prev, next dataprotection.SecretRow) (bool, error) {
	for i, r := range m.rows[t.Name] {
		if r.ID == prev.ID && r.SecretsEnc == prev.SecretsEnc {
			m.rows[t.Name][i] = next
			m.saves++
			return true, nil
		}
	}
	return false, nil
}

func (m *memSecrets) add(table string, r dataprotection.SecretRow) {
	if m.rows == nil {
		m.rows = map[string][]dataprotection.SecretRow{}
	}
	m.rows[table] = append(m.rows[table], r)
	sort.Slice(m.rows[table], func(i, j int) bool {
		a, b := m.rows[table][i].ID, m.rows[table][j].ID
		return bytes.Compare(a[:], b[:]) < 0
	})
}

type memPassports struct {
	calls []uuid.UUID
	pages [][]uuid.UUID
}

func (m *memPassports) ResealPassports(_ context.Context, _ string, after uuid.UUID, _ int, _ string, _ bool) (int, int, uuid.UUID, error) {
	m.calls = append(m.calls, after)
	if len(m.pages) == 0 {
		return 0, 0, after, nil
	}
	page := m.pages[0]
	m.pages = m.pages[1:]
	return len(page), len(page), page[len(page)-1], nil
}

type memAudit struct{ events []audit.RecordInput }

func (m *memAudit) Record(_ context.Context, in audit.RecordInput) error {
	m.events = append(m.events, in)
	return nil
}

func newService(kr *crypto.Keyring, s *memSecrets, p *memPassports, a *memAudit) *dataprotection.Service {
	return dataprotection.NewService(s, p, crypto.NewSecretBox(kr), kr, kr, a)
}

func TestBackfillMovesLegacySecretsAndIsIdempotent(t *testing.T) {
	kr := keyring(t, "k1")
	store := &memSecrets{}
	id, branch := uuid.New(), uuid.New()
	store.add("integration_accounts", dataprotection.SecretRow{
		ID: id, BranchID: branch,
		ConfigJSON: json.RawMessage(`{"page_id":"p-1","access_token":"tok-123456","verify_token":"v-1"}`),
	})
	rec := &memAudit{}
	svc := newService(kr, store, &memPassports{}, rec)

	res, err := svc.Run(context.Background(), uuid.Nil, dataprotection.Options{BatchSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Secrets["integration_accounts"] != 1 {
		t.Fatalf("result = %+v", res)
	}
	row := store.rows["integration_accounts"][0]
	var cfg map[string]string
	if err := json.Unmarshal(row.ConfigJSON, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg) != 1 || cfg["page_id"] != "p-1" {
		t.Fatalf("config_json = %v", cfg)
	}
	opened, err := crypto.NewSecretBox(kr).Open(row.SecretsEnc, crypto.Binding{Table: "integration_accounts", RowID: id, BranchID: branch})
	if err != nil || opened["access_token"] != "tok-123456" || opened["verify_token"] != "v-1" {
		t.Fatalf("opened = %v, %v", opened, err)
	}
	if row.VerifyTokenHash != kr.BlindIndex("v-1") {
		t.Fatal("verify token hash not set")
	}
	if len(rec.events) != 1 || rec.events[0].Action != "ops.encrypt_backfill" {
		t.Fatalf("audit = %+v", rec.events)
	}

	saves := store.saves
	if _, err := svc.Run(context.Background(), uuid.Nil, dataprotection.Options{}); err != nil {
		t.Fatal(err)
	}
	if store.saves != saves {
		t.Fatal("second run rewrote an already backfilled row")
	}
}

func TestBackfillReencryptsRetiredKey(t *testing.T) {
	old := keyring(t, "k0")
	id, branch := uuid.New(), uuid.New()
	b := crypto.Binding{Table: "ai_settings", RowID: id, BranchID: branch}
	sealed, err := crypto.NewSecretBox(old).Seal(map[string]string{"api_key": "sk-abcdef"}, b)
	if err != nil {
		t.Fatal(err)
	}
	store := &memSecrets{}
	store.add("ai_settings", dataprotection.SecretRow{ID: id, BranchID: branch, ConfigJSON: json.RawMessage(`{}`), SecretsEnc: sealed})

	kr := keyring(t, "k1", "k0")
	if _, err := newService(kr, store, &memPassports{}, &memAudit{}).Run(context.Background(), uuid.Nil, dataprotection.Options{}); err != nil {
		t.Fatal(err)
	}
	row := store.rows["ai_settings"][0]
	if !strings.HasPrefix(row.SecretsEnc, kr.ActivePrefix()) {
		t.Fatalf("not resealed under active key: %q", row.SecretsEnc)
	}
	opened, err := crypto.NewSecretBox(kr).Open(row.SecretsEnc, b)
	if err != nil || opened["api_key"] != "sk-abcdef" {
		t.Fatalf("opened = %v, %v", opened, err)
	}
}

func TestBackfillPassportsPagesUntilShortBatch(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	p := &memPassports{pages: [][]uuid.UUID{{a, b}, {c}}}
	res, err := newService(keyring(t, "k1"), &memSecrets{}, p, &memAudit{}).Run(context.Background(), uuid.Nil, dataprotection.Options{BatchSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passports["customers"] != 3 {
		t.Fatalf("result = %+v", res)
	}
	if len(p.calls) < 2 || p.calls[0] != uuid.Nil || p.calls[1] != b {
		t.Fatalf("cursor calls = %v", p.calls)
	}
}
