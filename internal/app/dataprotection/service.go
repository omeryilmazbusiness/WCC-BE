// Package dataprotection runs the idempotent encrypt backfill of Epic 20:
// it moves plaintext credentials out of config_json into sealed secrets_enc
// bags (T-259), encrypts plaintext passports (T-260) and re-encrypts
// ciphertexts sealed with a retired key. It may be rerun at any time.
package dataprotection

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/app/secretcfg"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
)

// SecretTable describes a table carrying a config_json/secrets_enc pair.
type SecretTable struct {
	Name string
	// VerifyTokenHash marks tables that also keep a blind index of the
	// verify_token secret (integration_accounts).
	VerifyTokenHash bool
}

// SecretTables are all tables with sealed secrets, in backfill order.
var SecretTables = []SecretTable{
	{Name: "integration_accounts", VerifyTokenHash: true},
	{Name: "ai_settings"},
}

// PassportTables are all tables with encrypted passports.
var PassportTables = []string{"customers", "booking_participants"}

// SecretRow is one stored config. For ai_settings ID is the branch id.
type SecretRow struct {
	ID              uuid.UUID
	BranchID        uuid.UUID
	ConfigJSON      json.RawMessage
	SecretsEnc      string
	VerifyTokenHash string
}

type SecretStore interface {
	// PendingSecrets returns up to limit rows with id > after, ordered by id,
	// that may hold plaintext secrets or ciphertext not sealed under
	// activePrefix.
	PendingSecrets(ctx context.Context, t SecretTable, after uuid.UUID, limit int, activePrefix string) ([]SecretRow, error)
	// SaveSecrets writes next only if the row still equals prev, returning
	// false when it changed concurrently (the next run picks it up).
	SaveSecrets(ctx context.Context, t SecretTable, prev, next SecretRow) (bool, error)
}

type PassportStore interface {
	// ResealPassports encrypts one batch (id > after, ordered by id) of rows
	// holding a plaintext passport, a ciphertext not sealed under
	// activePrefix or, with rehash, any ciphertext (to rebuild blind
	// indexes). It returns the rows scanned and rewritten and the last id.
	ResealPassports(ctx context.Context, table string, after uuid.UUID, limit int, activePrefix string, rehash bool) (scanned, updated int, last uuid.UUID, err error)
}

type Options struct {
	BatchSize int
	// Rehash rebuilds every passport blind index, e.g. after pinning
	// ENCRYPTION_BLIND_INDEX_KEY or rotating a key the index derives from.
	// Verify token indexes are always recomputed.
	Rehash bool
}

type Result struct {
	Secrets   map[string]int `json:"secrets"`
	Passports map[string]int `json:"passports"`
}

type Service struct {
	secrets   SecretStore
	passports PassportStore
	vault     secretcfg.Vault
	rotator   crypto.Rotator
	index     crypto.BlindIndexer
	audit     audit.Recorder
}

func NewService(secrets SecretStore, passports PassportStore, sealer crypto.SecretSealer, rotator crypto.Rotator, index crypto.BlindIndexer, rec audit.Recorder) *Service {
	return &Service{
		secrets: secrets, passports: passports, vault: secretcfg.NewVault(sealer),
		rotator: rotator, index: index, audit: rec,
	}
}

const defaultBatch = 200

// Run backfills every table; actorID is uuid.Nil for jobs.
func (s *Service) Run(ctx context.Context, actorID uuid.UUID, opt Options) (Result, error) {
	if opt.BatchSize <= 0 {
		opt.BatchSize = defaultBatch
	}
	res := Result{Secrets: map[string]int{}, Passports: map[string]int{}}
	for _, t := range SecretTables {
		n, err := s.backfillSecrets(ctx, t, opt)
		res.Secrets[t.Name] = n
		if err != nil {
			return res, err
		}
	}
	for _, t := range PassportTables {
		n, err := s.backfillPassports(ctx, t, opt)
		res.Passports[t] = n
		if err != nil {
			return res, err
		}
	}
	_ = s.audit.Record(ctx, audit.RecordInput{
		ActorID: actorID, Action: "ops.encrypt_backfill", EntityType: "data_protection",
		Extra: map[string]any{"secrets": res.Secrets, "passports": res.Passports, "rehash": opt.Rehash},
	})
	return res, nil
}

func (s *Service) backfillSecrets(ctx context.Context, t SecretTable, opt Options) (int, error) {
	updated := 0
	after := uuid.Nil
	prefix := s.rotator.ActivePrefix()
	for {
		rows, err := s.secrets.PendingSecrets(ctx, t, after, opt.BatchSize, prefix)
		if err != nil {
			return updated, err
		}
		for _, row := range rows {
			next, changed, err := s.resealSecrets(t, row)
			if err != nil {
				return updated, err
			}
			if !changed {
				continue
			}
			ok, err := s.secrets.SaveSecrets(ctx, t, row, next)
			if err != nil {
				return updated, err
			}
			if ok {
				updated++
			}
		}
		if len(rows) < opt.BatchSize {
			return updated, nil
		}
		after = rows[len(rows)-1].ID
	}
}

// resealSecrets computes the stored form of row: secrets out of config_json,
// sealed under the active key, verify token indexed.
func (s *Service) resealSecrets(t SecretTable, row SecretRow) (SecretRow, bool, error) {
	b := crypto.Binding{Table: t.Name, RowID: row.ID, BranchID: row.BranchID}
	cfg, err := s.vault.Load(row.ConfigJSON, row.SecretsEnc, b)
	if err != nil {
		return row, false, err
	}
	next := row
	next.ConfigJSON = cfg.Public
	legacy := !jsonEqual(row.ConfigJSON, cfg.Public)
	if legacy || s.rotator.NeedsRotation(row.SecretsEnc) {
		if next.SecretsEnc, err = s.vault.Seal(cfg, b); err != nil {
			return row, false, err
		}
	}
	if t.VerifyTokenHash {
		// Recomputed every run: an unpinned blind index key follows the
		// active encryption key.
		next.VerifyTokenHash = ""
		if tok := cfg.Secrets["verify_token"]; tok != "" {
			next.VerifyTokenHash = s.index.BlindIndex(tok)
		}
	}
	changed := legacy || next.SecretsEnc != row.SecretsEnc || next.VerifyTokenHash != row.VerifyTokenHash
	return next, changed, nil
}

func (s *Service) backfillPassports(ctx context.Context, table string, opt Options) (int, error) {
	updated := 0
	after := uuid.Nil
	prefix := s.rotator.ActivePrefix()
	for {
		scanned, n, last, err := s.passports.ResealPassports(ctx, table, after, opt.BatchSize, prefix, opt.Rehash)
		updated += n
		if err != nil {
			return updated, err
		}
		if scanned < opt.BatchSize {
			return updated, nil
		}
		if last == after {
			return updated, errors.New("dataprotection: passport backfill cursor did not advance")
		}
		after = last
	}
}

func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(orEmpty(a), &x) != nil || json.Unmarshal(orEmpty(b), &y) != nil {
		return false
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return string(xb) == string(yb)
}

func orEmpty(r json.RawMessage) json.RawMessage {
	if len(r) == 0 {
		return json.RawMessage(`{}`)
	}
	return r
}
