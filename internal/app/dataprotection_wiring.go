package app

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	pgaudit "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/audit"
	pgdataprotection "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/dataprotection"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgpii"
	pgprivacy "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/privacy"
	appaudit "github.com/wodi-crm/wodi-crm-be/internal/app/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/app/dataprotection"
	appprivacy "github.com/wodi-crm/wodi-crm-be/internal/app/privacy"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/ratelimit"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Passport reveals per user and window (T-261).
const (
	passportRevealLimit  = 30
	passportRevealWindow = time.Hour
)

// NewKeyring builds the at-rest encryption keyring shared by the API and the
// worker so both seal and blind-index identically.
func NewKeyring(a config.AuthConfig) (*crypto.Keyring, error) {
	kr, err := crypto.NewKeyring(a.EncryptionKeyID, a.EncryptionKey, a.PreviousEncryptionKeys)
	if err != nil {
		return nil, err
	}
	if a.BlindIndexKey != "" {
		if err := kr.UseBlindIndexKey(a.BlindIndexKey); err != nil {
			return nil, fmt.Errorf("ENCRYPTION_BLIND_INDEX_KEY: %w", err)
		}
	}
	return kr, nil
}

func newEncryptBackfill(pool *pgxpool.Pool, keyring *crypto.Keyring, rec audit.Recorder) *dataprotection.Service {
	store := pgdataprotection.NewStore(pool, pgpii.NewPassports(keyring))
	return dataprotection.NewService(store, store, crypto.NewSecretBox(keyring), keyring, keyring, rec)
}

// NewEncryptBackfill builds the encrypt backfill for processes outside the
// API (worker); the API wires the same service in New.
func NewEncryptBackfill(pool *pgxpool.Pool, keyring *crypto.Keyring) *dataprotection.Service {
	return newEncryptBackfill(pool, keyring, appaudit.NewService(pgaudit.NewRepository(pool)))
}

func newPrivacyService(
	pool *pgxpool.Pool,
	customers appprivacy.CustomerReader,
	bookings appprivacy.BookingReader,
	txm tx.Runner,
	rec audit.Recorder,
	limiter ratelimit.Window,
) *appprivacy.Service {
	svc := appprivacy.NewService(customers, bookings, pgprivacy.NewStore(pool), txm, rec)
	svc.SetRevealLimit(limiter, passportRevealLimit, passportRevealWindow)
	return svc
}
