-- +goose Up
-- Supplier management: category and emergency line, API integration with
-- sealed credentials and health, financial account (prepaid deposit, credit
-- line or card) with a ledger, default markups, coverage, contract dates,
-- API usage statistics and disputes.

ALTER TABLE suppliers
    ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT 'other'
        CHECK (category IN ('gds','wholesaler','dmc','transfer','visa','insurance','other')),
    ADD COLUMN IF NOT EXISTS emergency_phone TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS integration_type TEXT NOT NULL DEFAULT 'manual'
        CHECK (integration_type IN ('api','feed','manual')),
    ADD COLUMN IF NOT EXISTS environment TEXT NOT NULL DEFAULT 'sandbox'
        CHECK (environment IN ('sandbox','production')),
    ADD COLUMN IF NOT EXISTS api_base_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS webhook_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS credentials_enc TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS health_status TEXT NOT NULL DEFAULT 'unknown'
        CHECK (health_status IN ('unknown','active','degraded','down')),
    ADD COLUMN IF NOT EXISTS health_latency_ms INT NOT NULL DEFAULT 0 CHECK (health_latency_ms >= 0),
    ADD COLUMN IF NOT EXISTS health_checked_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS health_note TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS payment_model TEXT NOT NULL DEFAULT 'postpaid'
        CHECK (payment_model IN ('prepaid','postpaid','card')),
    ADD COLUMN IF NOT EXISTS currency TEXT NOT NULL DEFAULT 'SAR',
    ADD COLUMN IF NOT EXISTS deposit_balance BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS credit_limit BIGINT NOT NULL DEFAULT 0 CHECK (credit_limit >= 0),
    ADD COLUMN IF NOT EXISTS credit_used BIGINT NOT NULL DEFAULT 0 CHECK (credit_used >= 0),
    ADD COLUMN IF NOT EXISTS low_balance_threshold BIGINT NOT NULL DEFAULT 0 CHECK (low_balance_threshold >= 0),
    ADD COLUMN IF NOT EXISTS payment_terms TEXT NOT NULL DEFAULT 'net30'
        CHECK (payment_terms IN ('on_booking','net7','net15','net30','weekly')),
    ADD COLUMN IF NOT EXISTS markups JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS regions TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS free_cancel_hours INT NOT NULL DEFAULT 0
        CHECK (free_cancel_hours BETWEEN 0 AND 8760),
    ADD COLUMN IF NOT EXISTS contract_start DATE NULL,
    ADD COLUMN IF NOT EXISTS contract_end DATE NULL;

ALTER TABLE suppliers
    ADD CONSTRAINT suppliers_contract_range_check
        CHECK (contract_start IS NULL OR contract_end IS NULL OR contract_end >= contract_start);

CREATE INDEX IF NOT EXISTS idx_suppliers_category ON suppliers(branch_id, category, is_active);
CREATE INDEX IF NOT EXISTS idx_suppliers_contract_end ON suppliers(contract_end) WHERE contract_end IS NOT NULL;

CREATE TABLE IF NOT EXISTS supplier_ledger_entries (
    id             UUID PRIMARY KEY,
    supplier_id    UUID NOT NULL REFERENCES suppliers(id) ON DELETE CASCADE,
    branch_id      UUID NOT NULL REFERENCES branches(id),
    kind           TEXT NOT NULL CHECK (kind IN ('topup','charge','refund','payment','adjustment')),
    amount         BIGINT NOT NULL CHECK (amount <> 0),
    currency       TEXT NOT NULL,
    balance_after  BIGINT NOT NULL,
    reference      TEXT NOT NULL DEFAULT '',
    note           TEXT NOT NULL DEFAULT '',
    actor_id       UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_supplier_ledger_supplier ON supplier_ledger_entries(supplier_id, created_at DESC);

CREATE TABLE IF NOT EXISTS supplier_api_stats (
    supplier_id       UUID NOT NULL REFERENCES suppliers(id) ON DELETE CASCADE,
    day               DATE NOT NULL,
    searches          INT NOT NULL DEFAULT 0 CHECK (searches >= 0),
    bookings          INT NOT NULL DEFAULT 0 CHECK (bookings >= 0),
    errors            INT NOT NULL DEFAULT 0 CHECK (errors >= 0),
    price_changes     INT NOT NULL DEFAULT 0 CHECK (price_changes >= 0),
    sold_outs         INT NOT NULL DEFAULT 0 CHECK (sold_outs >= 0),
    latency_ms_total  BIGINT NOT NULL DEFAULT 0 CHECK (latency_ms_total >= 0),
    latency_samples   INT NOT NULL DEFAULT 0 CHECK (latency_samples >= 0),
    PRIMARY KEY (supplier_id, day)
);

CREATE TABLE IF NOT EXISTS supplier_disputes (
    id           UUID PRIMARY KEY,
    supplier_id  UUID NOT NULL REFERENCES suppliers(id) ON DELETE CASCADE,
    branch_id    UUID NOT NULL REFERENCES branches(id),
    title        TEXT NOT NULL,
    booking_ref  TEXT NOT NULL DEFAULT '',
    amount       BIGINT NOT NULL DEFAULT 0 CHECK (amount >= 0),
    currency     TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','rejected')),
    resolution   TEXT NOT NULL DEFAULT '',
    opened_by    UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    opened_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at  TIMESTAMPTZ NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_supplier_disputes_supplier ON supplier_disputes(supplier_id, status, opened_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_supplier_disputes_supplier;
DROP TABLE IF EXISTS supplier_disputes;
DROP TABLE IF EXISTS supplier_api_stats;
DROP INDEX IF EXISTS idx_supplier_ledger_supplier;
DROP TABLE IF EXISTS supplier_ledger_entries;
DROP INDEX IF EXISTS idx_suppliers_contract_end;
DROP INDEX IF EXISTS idx_suppliers_category;
ALTER TABLE suppliers DROP CONSTRAINT IF EXISTS suppliers_contract_range_check;
ALTER TABLE suppliers
    DROP COLUMN IF EXISTS contract_end,
    DROP COLUMN IF EXISTS contract_start,
    DROP COLUMN IF EXISTS free_cancel_hours,
    DROP COLUMN IF EXISTS regions,
    DROP COLUMN IF EXISTS markups,
    DROP COLUMN IF EXISTS payment_terms,
    DROP COLUMN IF EXISTS low_balance_threshold,
    DROP COLUMN IF EXISTS credit_used,
    DROP COLUMN IF EXISTS credit_limit,
    DROP COLUMN IF EXISTS deposit_balance,
    DROP COLUMN IF EXISTS currency,
    DROP COLUMN IF EXISTS payment_model,
    DROP COLUMN IF EXISTS health_note,
    DROP COLUMN IF EXISTS health_checked_at,
    DROP COLUMN IF EXISTS health_latency_ms,
    DROP COLUMN IF EXISTS health_status,
    DROP COLUMN IF EXISTS credentials_enc,
    DROP COLUMN IF EXISTS webhook_url,
    DROP COLUMN IF EXISTS api_base_url,
    DROP COLUMN IF EXISTS environment,
    DROP COLUMN IF EXISTS integration_type,
    DROP COLUMN IF EXISTS emergency_phone,
    DROP COLUMN IF EXISTS category;
