-- +goose Up
-- Finance hub: treasury accounts (cash, bank, POS, wallet) with an
-- append-only movement ledger and bank-feed matching, B2B agencies with
-- credit limits linked to bookings, departure budgets, sales commission
-- settings, BSP statement reconciliation and balance confirmation letters.

CREATE TABLE IF NOT EXISTS treasury_accounts (
    id                    UUID PRIMARY KEY,
    branch_id             UUID NOT NULL REFERENCES branches(id),
    kind                  TEXT NOT NULL CHECK (kind IN ('cash','bank','pos','wallet')),
    name                  TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    currency              TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    bank_name             TEXT NOT NULL DEFAULT '',
    iban                  TEXT NOT NULL DEFAULT '',
    commission_bps        INT NOT NULL DEFAULT 0 CHECK (commission_bps BETWEEN 0 AND 2000),
    balance               BIGINT NOT NULL DEFAULT 0,
    low_balance_threshold BIGINT NOT NULL DEFAULT 0 CHECK (low_balance_threshold >= 0),
    is_active             BOOLEAN NOT NULL DEFAULT TRUE,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_treasury_accounts_branch ON treasury_accounts(branch_id, is_active, currency);

CREATE TABLE IF NOT EXISTS treasury_movements (
    id                 UUID PRIMARY KEY,
    account_id         UUID NOT NULL REFERENCES treasury_accounts(id),
    branch_id          UUID NOT NULL REFERENCES branches(id),
    direction          TEXT NOT NULL CHECK (direction IN ('in','out')),
    kind               TEXT NOT NULL CHECK (kind IN ('collection','supplier_payment','transfer','expense','refund','adjustment')),
    amount             BIGINT NOT NULL CHECK (amount > 0),
    fee                BIGINT NOT NULL DEFAULT 0 CHECK (fee >= 0 AND fee <= amount),
    currency           TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    balance_after      BIGINT NOT NULL,
    booking_id         UUID NULL REFERENCES bookings(id) ON DELETE SET NULL,
    supplier_id        UUID NULL REFERENCES suppliers(id) ON DELETE SET NULL,
    counter_account_id UUID NULL REFERENCES treasury_accounts(id),
    transfer_id        UUID NULL,
    source             TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','bank_feed')),
    external_id        TEXT NOT NULL DEFAULT '',
    reference          TEXT NOT NULL DEFAULT '',
    counterparty       TEXT NOT NULL DEFAULT '',
    note               TEXT NOT NULL DEFAULT '',
    match_status       TEXT NOT NULL DEFAULT 'na' CHECK (match_status IN ('na','unmatched','matched','ignored')),
    matched_payment_id UUID NULL REFERENCES payments(id),
    occurred_on        DATE NOT NULL DEFAULT CURRENT_DATE,
    actor_id           UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_treasury_movements_account ON treasury_movements(account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_treasury_movements_branch_day ON treasury_movements(branch_id, occurred_on DESC);
CREATE INDEX IF NOT EXISTS idx_treasury_movements_unmatched ON treasury_movements(branch_id, created_at DESC) WHERE match_status = 'unmatched';
CREATE UNIQUE INDEX IF NOT EXISTS uq_treasury_movements_external ON treasury_movements(account_id, external_id) WHERE external_id <> '';

-- Movements are a ledger: money fields never change and rows are never
-- deleted; only the bank-feed match may be resolved once.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION treasury_movements_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'treasury ledger is append-only: DELETE rejected'
            USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.account_id IS DISTINCT FROM OLD.account_id
       OR NEW.direction IS DISTINCT FROM OLD.direction
       OR NEW.amount IS DISTINCT FROM OLD.amount
       OR NEW.fee IS DISTINCT FROM OLD.fee
       OR NEW.currency IS DISTINCT FROM OLD.currency
       OR NEW.balance_after IS DISTINCT FROM OLD.balance_after
       OR NEW.occurred_on IS DISTINCT FROM OLD.occurred_on
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'treasury movement % is immutable', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.match_status IS DISTINCT FROM OLD.match_status AND OLD.match_status <> 'unmatched' THEN
        RAISE EXCEPTION 'treasury movement % match is already resolved', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS trg_treasury_movements_guard ON treasury_movements;
CREATE TRIGGER trg_treasury_movements_guard
    BEFORE UPDATE OR DELETE ON treasury_movements
    FOR EACH ROW EXECUTE FUNCTION treasury_movements_guard();

CREATE TABLE IF NOT EXISTS agencies (
    id                 UUID PRIMARY KEY,
    branch_id          UUID NOT NULL REFERENCES branches(id),
    code               TEXT NOT NULL,
    name               TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 160),
    contact_name       TEXT NOT NULL DEFAULT '',
    phone              TEXT NOT NULL DEFAULT '',
    email              TEXT NOT NULL DEFAULT '',
    tax_id             TEXT NOT NULL DEFAULT '',
    currency           TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    credit_limit       BIGINT NOT NULL DEFAULT 0 CHECK (credit_limit >= 0),
    payment_terms_days INT NOT NULL DEFAULT 15 CHECK (payment_terms_days BETWEEN 0 AND 180),
    grace_days         INT NOT NULL DEFAULT 3 CHECK (grace_days BETWEEN 0 AND 90),
    auto_suspend       BOOLEAN NOT NULL DEFAULT TRUE,
    status             TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended','closed')),
    suspend_reason     TEXT NOT NULL DEFAULT '' CHECK (suspend_reason IN ('','manual','overdue')),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (branch_id, code)
);

CREATE INDEX IF NOT EXISTS idx_agencies_branch_status ON agencies(branch_id, status);

ALTER TABLE bookings ADD COLUMN IF NOT EXISTS agency_id UUID NULL REFERENCES agencies(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_bookings_agency ON bookings(agency_id) WHERE agency_id IS NOT NULL;

ALTER TABLE finance_settings
    ADD COLUMN IF NOT EXISTS commission_bps INT NOT NULL DEFAULT 1000 CHECK (commission_bps BETWEEN 0 AND 5000);

CREATE TABLE IF NOT EXISTS departure_budgets (
    departure_id UUID PRIMARY KEY REFERENCES departures(id) ON DELETE CASCADE,
    branch_id    UUID NOT NULL REFERENCES branches(id),
    currency     TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    revenue      BIGINT NOT NULL DEFAULT 0 CHECK (revenue >= 0),
    cost         BIGINT NOT NULL DEFAULT 0 CHECK (cost >= 0),
    note         TEXT NOT NULL DEFAULT '',
    updated_by   UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS bsp_statements (
    id             UUID PRIMARY KEY,
    branch_id      UUID NOT NULL REFERENCES branches(id),
    label          TEXT NOT NULL DEFAULT '',
    period_start   DATE NOT NULL,
    period_end     DATE NOT NULL,
    currency       TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    total          BIGINT NOT NULL DEFAULT 0,
    system_total   BIGINT NOT NULL DEFAULT 0,
    line_count     INT NOT NULL DEFAULT 0,
    matched        INT NOT NULL DEFAULT 0,
    mismatched     INT NOT NULL DEFAULT 0,
    missing_system INT NOT NULL DEFAULT 0,
    missing_bsp    INT NOT NULL DEFAULT 0,
    created_by     UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (period_end >= period_start)
);

CREATE INDEX IF NOT EXISTS idx_bsp_statements_branch ON bsp_statements(branch_id, period_end DESC);

CREATE TABLE IF NOT EXISTS bsp_lines (
    id            UUID PRIMARY KEY,
    statement_id  UUID NOT NULL REFERENCES bsp_statements(id) ON DELETE CASCADE,
    document_no   TEXT NOT NULL DEFAULT '',
    pnr           TEXT NOT NULL DEFAULT '',
    doc_type      TEXT NOT NULL DEFAULT 'sale' CHECK (doc_type IN ('sale','refund','adm','acm')),
    passenger     TEXT NOT NULL DEFAULT '',
    issued_on     DATE NULL,
    amount        BIGINT NOT NULL DEFAULT 0 CHECK (amount >= 0),
    booking_id    UUID NULL REFERENCES bookings(id) ON DELETE SET NULL,
    system_amount BIGINT NULL,
    status        TEXT NOT NULL CHECK (status IN ('matched','amount_mismatch','missing_in_system','missing_in_bsp')),
    sort_order    INT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_bsp_lines_statement ON bsp_lines(statement_id, sort_order);

CREATE TABLE IF NOT EXISTS reconciliation_letters (
    id            UUID PRIMARY KEY,
    branch_id     UUID NOT NULL REFERENCES branches(id),
    party_type    TEXT NOT NULL CHECK (party_type IN ('supplier','agency')),
    party_id      UUID NOT NULL,
    party_name    TEXT NOT NULL,
    period_end    DATE NOT NULL,
    balance       BIGINT NOT NULL,
    currency      TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    email         TEXT NOT NULL DEFAULT '',
    token_hash    TEXT NOT NULL UNIQUE,
    status        TEXT NOT NULL DEFAULT 'sent' CHECK (status IN ('sent','confirmed','disputed')),
    response_note TEXT NOT NULL DEFAULT '',
    responded_by  TEXT NOT NULL DEFAULT '',
    responded_at  TIMESTAMPTZ NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_by    UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((status = 'sent') = (responded_at IS NULL))
);

CREATE INDEX IF NOT EXISTS idx_reconciliation_letters_branch ON reconciliation_letters(branch_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_reconciliation_letters_party ON reconciliation_letters(party_type, party_id);

-- +goose Down
DROP INDEX IF EXISTS idx_reconciliation_letters_party;
DROP INDEX IF EXISTS idx_reconciliation_letters_branch;
DROP TABLE IF EXISTS reconciliation_letters;
DROP TABLE IF EXISTS bsp_lines;
DROP TABLE IF EXISTS bsp_statements;
DROP TABLE IF EXISTS departure_budgets;
ALTER TABLE finance_settings DROP COLUMN IF EXISTS commission_bps;
DROP INDEX IF EXISTS idx_bookings_agency;
ALTER TABLE bookings DROP COLUMN IF EXISTS agency_id;
DROP TABLE IF EXISTS agencies;
DROP TRIGGER IF EXISTS trg_treasury_movements_guard ON treasury_movements;
DROP FUNCTION IF EXISTS treasury_movements_guard();
DROP TABLE IF EXISTS treasury_movements;
DROP TABLE IF EXISTS treasury_accounts;
