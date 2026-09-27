-- +goose Up
-- Epic 20: encrypted integration secrets (T-259), encrypted passports (T-260), KVKK anonymization (T-267).
-- Ciphertexts are produced by the application keyring ("v1:<kid>:<b64>"), so existing plaintext is
-- moved by the app-level backfill (POST /v1/ops/encrypt-backfill or the security.encrypt_backfill job).

-- T-259: sealed secret bag per row; non-secret config (ids feeding external_account_id) stays in config_json.
ALTER TABLE integration_accounts
    ADD COLUMN IF NOT EXISTS secrets_enc TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS verify_token_hash TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_integration_accounts_verify_hash
    ON integration_accounts(provider, verify_token_hash) WHERE verify_token_hash <> '';

ALTER TABLE ai_settings ADD COLUMN IF NOT EXISTS secrets_enc TEXT NOT NULL DEFAULT '';
ALTER TABLE external_integrations ADD COLUMN IF NOT EXISTS secrets_enc TEXT NOT NULL DEFAULT '';
ALTER TABLE file_sync_connections ADD COLUMN IF NOT EXISTS secrets_enc TEXT NOT NULL DEFAULT '';

-- T-260: passport ciphertext, keyed blind index (dedupe/search) and display suffix.
-- passport_no is no longer written; it is dropped by a follow-up migration once the backfill is verified.
ALTER TABLE customers
    ADD COLUMN IF NOT EXISTS passport_enc TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS passport_hash TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS passport_last4 TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS anonymized_at TIMESTAMPTZ NULL;
CREATE INDEX IF NOT EXISTS idx_customers_passport_hash
    ON customers(branch_id, passport_hash) WHERE passport_hash <> '';

ALTER TABLE booking_participants
    ADD COLUMN IF NOT EXISTS passport_enc TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS passport_hash TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS passport_last4 TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_booking_participants_passport_hash
    ON booking_participants(passport_hash) WHERE passport_hash <> '';

-- +goose Down
-- After the backfill the ciphertext columns are the only copy of these values: dump them first.
DROP INDEX IF EXISTS idx_booking_participants_passport_hash;
ALTER TABLE booking_participants
    DROP COLUMN IF EXISTS passport_last4,
    DROP COLUMN IF EXISTS passport_hash,
    DROP COLUMN IF EXISTS passport_enc;
DROP INDEX IF EXISTS idx_customers_passport_hash;
ALTER TABLE customers
    DROP COLUMN IF EXISTS anonymized_at,
    DROP COLUMN IF EXISTS passport_last4,
    DROP COLUMN IF EXISTS passport_hash,
    DROP COLUMN IF EXISTS passport_enc;
ALTER TABLE file_sync_connections DROP COLUMN IF EXISTS secrets_enc;
ALTER TABLE external_integrations DROP COLUMN IF EXISTS secrets_enc;
ALTER TABLE ai_settings DROP COLUMN IF EXISTS secrets_enc;
DROP INDEX IF EXISTS idx_integration_accounts_verify_hash;
ALTER TABLE integration_accounts
    DROP COLUMN IF EXISTS verify_token_hash,
    DROP COLUMN IF EXISTS secrets_enc;
