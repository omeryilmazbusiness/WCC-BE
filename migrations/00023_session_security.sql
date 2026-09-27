-- +goose Up
-- Session security: server-side sessions (sid), token versions, opaque refresh tokens.

-- Bumped whenever claims carried in access tokens (or the right to hold one) change.
ALTER TABLE users ADD COLUMN IF NOT EXISTS token_version INT NOT NULL DEFAULT 1;

-- One row per signed-in device; id is the refresh-token family id and the access-token sid.
CREATE TABLE IF NOT EXISTS auth_sessions (
    id                   UUID PRIMARY KEY,
    user_id              UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    absolute_expires_at  TIMESTAMPTZ NOT NULL,
    idle_expires_at      TIMESTAMPTZ NOT NULL,
    last_seen_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_ip              TEXT NOT NULL DEFAULT '',
    user_agent           TEXT NOT NULL DEFAULT '',
    auth_method          TEXT NOT NULL CHECK (auth_method IN ('password','totp','recovery','mfa_setup')),
    revoked_at           TIMESTAMPTZ,
    revoked_reason       TEXT NOT NULL DEFAULT '',
    CHECK (idle_expires_at <= absolute_expires_at)
);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_user_active ON auth_sessions(user_id, created_at DESC) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_auth_sessions_revoked ON auth_sessions(revoked_at) WHERE revoked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_auth_sessions_idle ON auth_sessions(idle_expires_at);

-- Refresh tokens were signed JWTs and are now opaque, so none can be presented again:
-- keep their history under revoked sessions and end them.
INSERT INTO auth_sessions (id, user_id, created_at, absolute_expires_at, idle_expires_at, last_seen_at,
                           last_ip, user_agent, auth_method, revoked_at, revoked_reason)
SELECT family_id,
       (array_agg(user_id ORDER BY created_at))[1],
       MIN(created_at), MAX(expires_at), MAX(expires_at), MAX(created_at),
       '', '', 'password', NOW(), 'migrated'
FROM refresh_tokens
GROUP BY family_id
ON CONFLICT (id) DO NOTHING;

UPDATE refresh_tokens SET revoked_at = NOW(), revoked_reason = 'migrated' WHERE revoked_at IS NULL;

ALTER TABLE refresh_tokens
    ADD CONSTRAINT fk_refresh_tokens_session FOREIGN KEY (family_id) REFERENCES auth_sessions(id) ON DELETE CASCADE;

-- Retention deletes old tokens in batches; a chain link must not block it.
ALTER TABLE refresh_tokens DROP CONSTRAINT IF EXISTS refresh_tokens_replaced_by_fkey;
ALTER TABLE refresh_tokens
    ADD CONSTRAINT refresh_tokens_replaced_by_fkey FOREIGN KEY (replaced_by) REFERENCES refresh_tokens(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_revoked ON refresh_tokens(revoked_at) WHERE revoked_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_webhook_events_received ON webhook_events(received_at);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION bump_user_token_version() RETURNS trigger AS $$
BEGIN
    IF NEW.role IS DISTINCT FROM OLD.role
       OR NEW.branch_id IS DISTINCT FROM OLD.branch_id
       OR NEW.team_id IS DISTINCT FROM OLD.team_id
       OR NEW.is_active IS DISTINCT FROM OLD.is_active
       OR NEW.password_hash IS DISTINCT FROM OLD.password_hash THEN
        NEW.token_version := OLD.token_version + 1;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS trg_users_token_version ON users;
CREATE TRIGGER trg_users_token_version
    BEFORE UPDATE OF role, branch_id, team_id, is_active, password_hash ON users
    FOR EACH ROW EXECUTE FUNCTION bump_user_token_version();

-- Password change or deactivation ends every session, whichever code path updates users.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION revoke_refresh_tokens_on_credential_change() RETURNS trigger AS $$
DECLARE
    reason TEXT;
BEGIN
    IF NEW.password_hash IS DISTINCT FROM OLD.password_hash
       OR (OLD.is_active AND NOT NEW.is_active) THEN
        reason := CASE WHEN NEW.is_active THEN 'password_changed' ELSE 'user_deactivated' END;
        UPDATE auth_sessions SET revoked_at = NOW(), revoked_reason = reason
         WHERE user_id = NEW.id AND revoked_at IS NULL;
        UPDATE refresh_tokens SET revoked_at = NOW(), revoked_reason = reason
         WHERE user_id = NEW.id AND revoked_at IS NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
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
DROP TRIGGER IF EXISTS trg_users_token_version ON users;
DROP FUNCTION IF EXISTS bump_user_token_version();
DROP INDEX IF EXISTS idx_webhook_events_received;
DROP INDEX IF EXISTS idx_refresh_tokens_revoked;
ALTER TABLE refresh_tokens DROP CONSTRAINT IF EXISTS refresh_tokens_replaced_by_fkey;
ALTER TABLE refresh_tokens
    ADD CONSTRAINT refresh_tokens_replaced_by_fkey FOREIGN KEY (replaced_by) REFERENCES refresh_tokens(id);
ALTER TABLE refresh_tokens DROP CONSTRAINT IF EXISTS fk_refresh_tokens_session;
DELETE FROM refresh_tokens;
DROP TABLE IF EXISTS auth_sessions;
ALTER TABLE users DROP COLUMN IF EXISTS token_version;
