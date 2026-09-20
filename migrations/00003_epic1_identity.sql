-- +goose Up
-- Epic 1: full roles, teams, MFA hooks columns, audit list indexes

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
    CHECK (role IN ('gm', 'manager', 'employee', 'finance', 'operations', 'admin'));

CREATE TABLE teams (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id   UUID NOT NULL REFERENCES branches(id),
    code        TEXT NOT NULL,
    name_en     TEXT NOT NULL,
    name_ar     TEXT NOT NULL DEFAULT '',
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (branch_id, code)
);

CREATE INDEX idx_teams_branch ON teams(branch_id);

-- Link users.team_id → teams (was free UUID before)
ALTER TABLE users
    ADD CONSTRAINT users_team_id_fkey
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE SET NULL;

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS mfa_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS mfa_secret_enc TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_audit_events_created ON audit_events(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_action ON audit_events(action, created_at DESC);

INSERT INTO teams (id, branch_id, code, name_en, name_ar)
VALUES
    ('33333333-3333-3333-3333-333333333301',
     '11111111-1111-1111-1111-111111111111',
     'SALES', 'Sales Team', 'فريق المبيعات')
ON CONFLICT DO NOTHING;

UPDATE users
SET team_id = '33333333-3333-3333-3333-333333333301'
WHERE email = 'sales@wodi.local' AND team_id IS NULL;

INSERT INTO users (id, email, password_hash, full_name, role, branch_id, is_active)
VALUES
    ('22222222-2222-2222-2222-222222222204', 'admin@wodi.local',
     '$2a$10$5CR8wFjAGXO1HO3elXTwvuEi1CGjWD74PK6foes35Zf2emMDpOzhK',
     'System Admin', 'admin', '11111111-1111-1111-1111-111111111111', TRUE),
    ('22222222-2222-2222-2222-222222222205', 'finance@wodi.local',
     '$2a$10$5CR8wFjAGXO1HO3elXTwvuEi1CGjWD74PK6foes35Zf2emMDpOzhK',
     'Finance User', 'finance', '11111111-1111-1111-1111-111111111111', TRUE),
    ('22222222-2222-2222-2222-222222222206', 'ops@wodi.local',
     '$2a$10$5CR8wFjAGXO1HO3elXTwvuEi1CGjWD74PK6foes35Zf2emMDpOzhK',
     'Operations User', 'operations', '11111111-1111-1111-1111-111111111111', TRUE)
ON CONFLICT (email) DO NOTHING;

-- +goose Down
DELETE FROM users WHERE email IN ('admin@wodi.local', 'finance@wodi.local', 'ops@wodi.local');
UPDATE users SET team_id = NULL WHERE team_id = '33333333-3333-3333-3333-333333333301';
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_team_id_fkey;
DROP TABLE IF EXISTS teams;
ALTER TABLE users DROP COLUMN IF EXISTS mfa_enabled;
ALTER TABLE users DROP COLUMN IF EXISTS mfa_secret_enc;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
    CHECK (role IN ('gm', 'manager', 'employee'));
DROP INDEX IF EXISTS idx_audit_events_created;
DROP INDEX IF EXISTS idx_audit_events_action;
