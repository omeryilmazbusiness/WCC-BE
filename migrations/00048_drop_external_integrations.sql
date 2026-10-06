-- +goose Up
-- External accounting / GDS / payment gateway stubs are no longer offered.
DROP INDEX IF EXISTS idx_external_integrations_branch;
DROP TABLE IF EXISTS external_integrations;

-- +goose Down
CREATE TABLE IF NOT EXISTS external_integrations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id       UUID NOT NULL REFERENCES branches(id),
    kind            TEXT NOT NULL
        CHECK (kind IN ('accounting','gds','payment_gateway')),
    provider_key    TEXT NOT NULL DEFAULT '',
    display_name    TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'stub'
        CHECK (status IN ('stub','configured','disabled','error')),
    health          TEXT NOT NULL DEFAULT 'unknown'
        CHECK (health IN ('unknown','ok','degraded','down')),
    config_json     JSONB NOT NULL DEFAULT '{}'::jsonb,
    secrets_enc     TEXT NOT NULL DEFAULT '',
    last_checked_at TIMESTAMPTZ NULL,
    last_error      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (branch_id, kind, provider_key)
);

CREATE INDEX IF NOT EXISTS idx_external_integrations_branch
    ON external_integrations(branch_id, kind);
