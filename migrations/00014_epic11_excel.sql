-- +goose Up
-- Epic 11: Excel Import/Export

CREATE TABLE IF NOT EXISTS import_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id       UUID NOT NULL REFERENCES branches(id),
    entity_type     TEXT NOT NULL CHECK (entity_type IN ('customers','bookings','payments','departures')),
    mode            TEXT NOT NULL DEFAULT 'upsert' CHECK (mode IN ('create','update','upsert')),
    status          TEXT NOT NULL DEFAULT 'uploaded'
        CHECK (status IN ('uploaded','mapped','validated','queued','processing','completed','failed')),
    file_name       TEXT NOT NULL DEFAULT '',
    content_type    TEXT NOT NULL DEFAULT '',
    file_bytes      BYTEA,
    storage_key     TEXT NOT NULL DEFAULT '',
    headers_json    JSONB NOT NULL DEFAULT '[]'::jsonb,
    mapping_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    preview_json    JSONB NOT NULL DEFAULT '[]'::jsonb,
    total_rows      INT NOT NULL DEFAULT 0,
    success_count   INT NOT NULL DEFAULT 0,
    failed_count    INT NOT NULL DEFAULT 0,
    skipped_count   INT NOT NULL DEFAULT 0,
    rollback_token  UUID NOT NULL DEFAULT gen_random_uuid(),
    error_message   TEXT NOT NULL DEFAULT '',
    created_by      UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_import_jobs_branch_created
    ON import_jobs(branch_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_import_jobs_status
    ON import_jobs(status);

CREATE TABLE IF NOT EXISTS import_row_errors (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id      UUID NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    row_number  INT NOT NULL,
    field       TEXT NOT NULL DEFAULT '',
    message     TEXT NOT NULL,
    raw_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_import_row_errors_job
    ON import_row_errors(job_id, row_number);

CREATE TABLE IF NOT EXISTS import_mapping_templates (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id    UUID NOT NULL REFERENCES branches(id),
    name         TEXT NOT NULL,
    entity_type  TEXT NOT NULL CHECK (entity_type IN ('customers','bookings','payments','departures')),
    mapping_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by   UUID NOT NULL REFERENCES users(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (branch_id, entity_type, name)
);

CREATE INDEX IF NOT EXISTS idx_import_templates_branch
    ON import_mapping_templates(branch_id, entity_type);

-- +goose Down
DROP TABLE IF EXISTS import_mapping_templates;
DROP TABLE IF EXISTS import_row_errors;
DROP TABLE IF EXISTS import_jobs;
