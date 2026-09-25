-- +goose Up
-- Epic 16: P2 Advanced Extensions — connected file sync, supplier invoices, external stubs

-- T-202: Connected Excel sync (OneDrive / SharePoint)
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

-- T-203: Advanced supplier cost / invoice tracking
CREATE TABLE IF NOT EXISTS supplier_invoices (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id       UUID NOT NULL REFERENCES branches(id),
    supplier_id     UUID NOT NULL REFERENCES suppliers(id) ON DELETE CASCADE,
    invoice_number  TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft','submitted','approved','paid','void')),
    currency        TEXT NOT NULL DEFAULT 'SAR',
    subtotal        BIGINT NOT NULL DEFAULT 0,
    tax_total       BIGINT NOT NULL DEFAULT 0,
    grand_total     BIGINT NOT NULL DEFAULT 0,
    issued_on       DATE NULL,
    due_on          DATE NULL,
    paid_at         TIMESTAMPTZ NULL,
    notes           TEXT NOT NULL DEFAULT '',
    created_by      UUID NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_supplier_invoice_number
    ON supplier_invoices(branch_id, supplier_id, invoice_number)
    WHERE invoice_number <> '';
CREATE INDEX IF NOT EXISTS idx_supplier_invoices_supplier
    ON supplier_invoices(supplier_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_supplier_invoices_branch
    ON supplier_invoices(branch_id, status, due_on);

CREATE TABLE IF NOT EXISTS supplier_invoice_lines (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id      UUID NOT NULL REFERENCES supplier_invoices(id) ON DELETE CASCADE,
    link_id         UUID NULL REFERENCES supplier_links(id) ON DELETE SET NULL,
    description     TEXT NOT NULL DEFAULT '',
    quantity        INT NOT NULL DEFAULT 1,
    unit_cost       BIGINT NOT NULL DEFAULT 0,
    line_total      BIGINT NOT NULL DEFAULT 0,
    sort_order      INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_supplier_invoice_lines
    ON supplier_invoice_lines(invoice_id, sort_order);

-- T-204: External accounting / GDS / payment gateway stubs
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
    last_checked_at TIMESTAMPTZ NULL,
    last_error      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (branch_id, kind, provider_key)
);

CREATE INDEX IF NOT EXISTS idx_external_integrations_branch
    ON external_integrations(branch_id, kind);

-- +goose Down
DROP INDEX IF EXISTS idx_external_integrations_branch;
DROP TABLE IF EXISTS external_integrations;
DROP INDEX IF EXISTS idx_supplier_invoice_lines;
DROP TABLE IF EXISTS supplier_invoice_lines;
DROP INDEX IF EXISTS idx_supplier_invoices_branch;
DROP INDEX IF EXISTS idx_supplier_invoices_supplier;
DROP INDEX IF EXISTS uq_supplier_invoice_number;
DROP TABLE IF EXISTS supplier_invoices;
DROP INDEX IF EXISTS idx_file_sync_runs_branch;
DROP INDEX IF EXISTS idx_file_sync_runs_conn;
DROP TABLE IF EXISTS file_sync_runs;
DROP INDEX IF EXISTS idx_file_sync_conn_branch;
DROP TABLE IF EXISTS file_sync_connections;
