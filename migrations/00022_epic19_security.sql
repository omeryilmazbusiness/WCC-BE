-- +goose Up
-- Epic 19: TOTP MFA, login lockout, refresh-token rotation, webhook ingress hardening.

-- T-251 / T-253: MFA + lockout state on users.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS mfa_pending_secret_enc TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS mfa_last_step          BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS mfa_enrolled_at        TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS failed_login_count     INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_failed_login_at   TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS lockout_count          INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS locked_until           TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS mfa_recovery_codes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash   TEXT NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, code_hash)
);

CREATE TABLE IF NOT EXISTS mfa_challenges (
    token_hash  TEXT PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose     TEXT NOT NULL CHECK (purpose IN ('login','enroll')),
    attempts    INT NOT NULL DEFAULT 0,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_ip  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_mfa_challenges_user ON mfa_challenges(user_id);
CREATE INDEX IF NOT EXISTS idx_mfa_challenges_expires ON mfa_challenges(expires_at);

-- T-255: refresh-token rotation with family-wide reuse detection.
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id       UUID NOT NULL,
    token_hash      TEXT NOT NULL UNIQUE,
    expires_at      TIMESTAMPTZ NOT NULL,
    revoked_at      TIMESTAMPTZ,
    revoked_reason  TEXT NOT NULL DEFAULT '',
    replaced_by     UUID REFERENCES refresh_tokens(id),
    created_ip      TEXT NOT NULL DEFAULT '',
    user_agent      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_family ON refresh_tokens(family_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_active ON refresh_tokens(user_id) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires ON refresh_tokens(expires_at);

-- Password change or deactivation revokes every session, whichever code path updates users.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION revoke_refresh_tokens_on_credential_change() RETURNS trigger AS $$
BEGIN
    IF NEW.password_hash IS DISTINCT FROM OLD.password_hash
       OR (OLD.is_active AND NOT NEW.is_active) THEN
        UPDATE refresh_tokens
           SET revoked_at = NOW(),
               revoked_reason = CASE WHEN NEW.is_active THEN 'password_changed' ELSE 'user_deactivated' END
         WHERE user_id = NEW.id AND revoked_at IS NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS trg_users_revoke_refresh ON users;
CREATE TRIGGER trg_users_revoke_refresh
    AFTER UPDATE OF password_hash, is_active ON users
    FOR EACH ROW EXECUTE FUNCTION revoke_refresh_tokens_on_credential_change();

-- T-249: route webhooks to a branch via the provider-side account id.
-- Derived from config_json so every connect/disconnect path stays consistent;
-- config_json.external_account_id overrides the per-provider default.
ALTER TABLE integration_accounts
    ADD COLUMN IF NOT EXISTS external_account_id TEXT GENERATED ALWAYS AS (
        COALESCE(
            NULLIF(config_json->>'external_account_id', ''),
            CASE provider
                WHEN 'whatsapp'  THEN NULLIF(config_json->>'phone_number_id', '')
                WHEN 'instagram' THEN COALESCE(NULLIF(config_json->>'ig_user_id', ''), NULLIF(config_json->>'page_id', ''))
                WHEN 'facebook'  THEN NULLIF(config_json->>'page_id', '')
                WHEN 'gmail'     THEN NULLIF(lower(config_json->>'mailbox_email'), '')
                WHEN 'email'     THEN NULLIF(lower(config_json->>'mailbox_email'), '')
            END
        )
    ) STORED;
CREATE UNIQUE INDEX IF NOT EXISTS uq_integration_accounts_provider_external
    ON integration_accounts(provider, external_account_id)
    WHERE external_account_id IS NOT NULL;

-- T-250: raw webhook journal. Rejected rows never claim an event id, so a
-- forged request cannot pre-empt the genuine delivery.
CREATE TABLE IF NOT EXISTS webhook_events (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider             TEXT NOT NULL,
    external_event_id    TEXT NOT NULL,
    external_account_id  TEXT NOT NULL DEFAULT '',
    integration_account_id UUID REFERENCES integration_accounts(id) ON DELETE SET NULL,
    branch_id            UUID REFERENCES branches(id),
    signature_valid      BOOLEAN NOT NULL DEFAULT FALSE,
    status               TEXT NOT NULL DEFAULT 'received'
                         CHECK (status IN ('received','processed','failed','rejected')),
    error                TEXT NOT NULL DEFAULT '',
    source_ip            TEXT NOT NULL DEFAULT '',
    payload              JSONB NOT NULL DEFAULT '{}'::jsonb,
    received_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at         TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_webhook_events_provider_event
    ON webhook_events(provider, external_event_id)
    WHERE status <> 'rejected';
CREATE INDEX IF NOT EXISTS idx_webhook_events_status ON webhook_events(status, received_at DESC);
CREATE INDEX IF NOT EXISTS idx_webhook_events_branch ON webhook_events(branch_id, received_at DESC);

-- +goose Down
DROP TABLE IF EXISTS webhook_events;
DROP INDEX IF EXISTS uq_integration_accounts_provider_external;
ALTER TABLE integration_accounts DROP COLUMN IF EXISTS external_account_id;
DROP TRIGGER IF EXISTS trg_users_revoke_refresh ON users;
DROP FUNCTION IF EXISTS revoke_refresh_tokens_on_credential_change();
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS mfa_challenges;
DROP TABLE IF EXISTS mfa_recovery_codes;
ALTER TABLE users
    DROP COLUMN IF EXISTS locked_until,
    DROP COLUMN IF EXISTS lockout_count,
    DROP COLUMN IF EXISTS last_failed_login_at,
    DROP COLUMN IF EXISTS failed_login_count,
    DROP COLUMN IF EXISTS mfa_enrolled_at,
    DROP COLUMN IF EXISTS mfa_last_step,
    DROP COLUMN IF EXISTS mfa_pending_secret_enc;
