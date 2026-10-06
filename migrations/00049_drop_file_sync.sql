-- +goose Up
-- OneDrive / SharePoint file sync is no longer offered.
DROP INDEX IF EXISTS idx_file_sync_runs_branch;
DROP INDEX IF EXISTS idx_file_sync_runs_conn;
DROP TABLE IF EXISTS file_sync_runs;
DROP INDEX IF EXISTS idx_file_sync_conn_branch;
DROP TABLE IF EXISTS file_sync_connections;

-- +goose Down
CREATE TABLE IF NOT EXISTS file_sync_connections (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id           UUID NOT NULL REFERENCES branches(id),
    provider            TEXT NOT NULL
        CHECK (provider IN ('onedrive','sharepoint')),
    display_name        TEXT NOT NULL DEFAULT '',
    remote_path         TEXT NOT NULL DEFAULT '',
    entity_type         TEXT NOT NULL DEFAULT 'customers'
        CHECK (entity_type IN ('customers','bookings','payments','departures')),
    source_of_truth     TEXT NOT NULL DEFAULT 'platform'
        CHECK (source_of_truth IN ('platform','file','manual_review')),
    conflict_policy     TEXT NOT NULL DEFAULT 'prefer_platform'
        CHECK (conflict_policy IN ('prefer_platform','prefer_file','flag')),
    enabled             BOOLEAN NOT NULL DEFAULT TRUE,
    status              TEXT NOT NULL DEFAULT 'disconnected'
        CHECK (status IN ('disconnected','connected','error','syncing')),
    last_sync_at        TIMESTAMPTZ NULL,
    last_error          TEXT NOT NULL DEFAULT '',
    config_json         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by          UUID NULL REFERENCES users(id),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_file_sync_conn_branch
    ON file_sync_connections(branch_id, enabled);

CREATE TABLE IF NOT EXISTS file_sync_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id   UUID NOT NULL REFERENCES file_sync_connections(id) ON DELETE CASCADE,
    branch_id       UUID NOT NULL REFERENCES branches(id),
    actor_id        UUID NULL REFERENCES users(id),
    direction       TEXT NOT NULL DEFAULT 'pull'
        CHECK (direction IN ('pull','push','bidirectional')),
    status          TEXT NOT NULL DEFAULT 'running'
        CHECK (status IN ('running','ok','error','conflicts')),
    rows_read       INT NOT NULL DEFAULT 0,
    rows_applied    INT NOT NULL DEFAULT 0,
    conflicts       INT NOT NULL DEFAULT 0,
    summary_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message   TEXT NOT NULL DEFAULT '',
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at     TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_file_sync_runs_conn
    ON file_sync_runs(connection_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_file_sync_runs_branch
    ON file_sync_runs(branch_id, started_at DESC);

ALTER TABLE file_sync_connections ADD COLUMN IF NOT EXISTS secrets_enc TEXT NOT NULL DEFAULT '';
