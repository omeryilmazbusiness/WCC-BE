-- +goose Up
-- Epic 14: Reporting hub — integration logs + auditable report exports

CREATE TABLE IF NOT EXISTS integration_logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id       UUID NOT NULL REFERENCES branches(id),
    provider        TEXT NOT NULL DEFAULT '',
    direction       TEXT NOT NULL DEFAULT 'inbound'
        CHECK (direction IN ('inbound','outbound','health','system')),
    status          TEXT NOT NULL DEFAULT 'ok'
        CHECK (status IN ('ok','error','retry','degraded')),
    summary         TEXT NOT NULL DEFAULT '',
    detail          TEXT NOT NULL DEFAULT '',
    correlation_id  TEXT NOT NULL DEFAULT '',
    entity_type     TEXT NOT NULL DEFAULT '',
    entity_id       UUID NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_integration_logs_branch_created
    ON integration_logs(branch_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_integration_logs_provider_status
    ON integration_logs(branch_id, provider, status, created_at DESC);

CREATE TABLE IF NOT EXISTS report_export_audits (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id     UUID NOT NULL REFERENCES branches(id),
    actor_id      UUID NOT NULL REFERENCES users(id),
    report_kind   TEXT NOT NULL,
    filters_json  JSONB NOT NULL DEFAULT '{}'::jsonb,
    row_count     INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_report_export_audits_branch
    ON report_export_audits(branch_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS report_export_audits;
DROP TABLE IF EXISTS integration_logs;
