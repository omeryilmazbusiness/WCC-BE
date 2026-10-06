// Package dataprotection is the postgres store of the encrypt backfill. It
// works across branches (system job), never through a caller scope.
package dataprotection

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgpii"
	app "github.com/wodi-crm/wodi-crm-be/internal/app/dataprotection"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// secretIDColumns whitelists the tables (and their row key) the backfill may
// touch; table names are interpolated into SQL.
var secretIDColumns = map[string]string{
	"integration_accounts": "id",
	"ai_settings":          "branch_id",
}

var passportTables = map[string]bool{
	pgpii.Customers:           true,
	pgpii.BookingParticipants: true,
}

type Store struct {
	pool *pgxpool.Pool
	pii  *pgpii.Passports
}

func NewStore(pool *pgxpool.Pool, pii *pgpii.Passports) *Store {
	return &Store{pool: pool, pii: pii}
}

func secretIDColumn(t app.SecretTable) (string, error) {
	col, ok := secretIDColumns[t.Name]
	if !ok {
		return "", fmt.Errorf("dataprotection: unknown secret table %q", t.Name)
	}
	return col, nil
}

func (s *Store) PendingSecrets(ctx context.Context, t app.SecretTable, after uuid.UUID, limit int, activePrefix string) ([]app.SecretRow, error) {
	idCol, err := secretIDColumn(t)
	if err != nil {
		return nil, err
	}
	hashCol, sealedRows := `''`, ``
	if t.VerifyTokenHash {
		hashCol, sealedRows = `verify_token_hash`, ` OR secrets_enc <> ''`
	}
	q := tx.QuerierFrom(ctx, s.pool)
	rows, err := q.Query(ctx, `
		SELECT `+idCol+`, branch_id, COALESCE(config_json,'{}'::jsonb), secrets_enc, `+hashCol+`
		FROM `+t.Name+`
		WHERE `+idCol+` > $1
		  AND (COALESCE(config_json,'{}'::jsonb) <> '{}'::jsonb
		    OR (secrets_enc <> '' AND left(secrets_enc, length($2)) <> $2)`+sealedRows+`)
		ORDER BY `+idCol+` LIMIT $3`, after, activePrefix, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []app.SecretRow
	for rows.Next() {
		var r app.SecretRow
		var cfg []byte
		if err := rows.Scan(&r.ID, &r.BranchID, &cfg, &r.SecretsEnc, &r.VerifyTokenHash); err != nil {
			return nil, err
		}
		r.ConfigJSON = cfg
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) SaveSecrets(ctx context.Context, t app.SecretTable, prev, next app.SecretRow) (bool, error) {
	idCol, err := secretIDColumn(t)
	if err != nil {
		return false, err
	}
	setHash := ``
	args := []any{prev.ID, string(orEmpty(next.ConfigJSON)), next.SecretsEnc, prev.SecretsEnc, string(orEmpty(prev.ConfigJSON))}
	if t.VerifyTokenHash {
		setHash = `, verify_token_hash = $6`
		args = append(args, next.VerifyTokenHash)
	}
	q := tx.QuerierFrom(ctx, s.pool)
	tag, err := q.Exec(ctx, `
		UPDATE `+t.Name+` SET config_json = $2::jsonb, secrets_enc = $3`+setHash+`
		WHERE `+idCol+` = $1 AND secrets_enc = $4 AND COALESCE(config_json,'{}'::jsonb) = $5::jsonb`, args...)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) ResealPassports(ctx context.Context, table string, after uuid.UUID, limit int, activePrefix string, rehash bool) (int, int, uuid.UUID, error) {
	if !passportTables[table] {
		return 0, 0, after, fmt.Errorf("dataprotection: unknown passport table %q", table)
	}
	q := tx.QuerierFrom(ctx, s.pool)
	rows, err := q.Query(ctx, `
		SELECT id, COALESCE(passport_no,''), passport_enc, passport_hash
		FROM `+table+`
		WHERE id > $1
		  AND (COALESCE(passport_no,'') <> ''
		    OR (passport_enc <> '' AND left(passport_enc, length($2)) <> $2)
		    OR ($3 AND passport_enc <> ''))
		ORDER BY id LIMIT $4`, after, activePrefix, rehash, limit)
	if err != nil {
		return 0, 0, after, err
	}
	type pending struct {
		id                uuid.UUID
		legacy, enc, hash string
	}
	var batch []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.legacy, &p.enc, &p.hash); err != nil {
			rows.Close()
			return 0, 0, after, err
		}
		batch = append(batch, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, after, err
	}

	updated := 0
	last := after
	for _, p := range batch {
		last = p.id
		plain, err := s.pii.Open(table, p.id, p.enc, p.legacy)
		if err != nil {
			return len(batch), updated, last, fmt.Errorf("dataprotection: %s %s: %w", table, p.id, err)
		}
		stale := p.enc != "" && !strings.HasPrefix(p.enc, activePrefix)
		if p.legacy == "" && !stale && s.pii.Hash(plain) == p.hash {
			continue
		}
		sealed, err := s.pii.Seal(table, p.id, plain)
		if err != nil {
			return len(batch), updated, last, err
		}
		tag, err := q.Exec(ctx, `
			UPDATE `+table+` SET passport_no = '', passport_enc = $2, passport_hash = $3, passport_last4 = $4
			WHERE id = $1 AND COALESCE(passport_no,'') = $5 AND passport_enc = $6`,
			p.id, sealed.Enc, sealed.Hash, sealed.Last4, p.legacy, p.enc)
		if err != nil {
			return len(batch), updated, last, err
		}
		if tag.RowsAffected() == 1 {
			updated++
		}
	}
	return len(batch), updated, last, nil
}

func orEmpty(r json.RawMessage) json.RawMessage {
	if len(r) == 0 {
		return json.RawMessage(`{}`)
	}
	return r
}
