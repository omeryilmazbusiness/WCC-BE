-- +goose Up
-- Epic 21 finance: FX rate table, reporting-currency snapshots on payments and
-- bookings, payment received date, payment promises.

CREATE TABLE IF NOT EXISTS fx_rates (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    base            TEXT NOT NULL CHECK (base ~ '^[A-Z]{3}$'),
    quote           TEXT NOT NULL CHECK (quote ~ '^[A-Z]{3}$'),
    rate_scaled     BIGINT NOT NULL CHECK (rate_scaled > 0),
    effective_date  DATE NOT NULL,
    source          TEXT NOT NULL DEFAULT 'manual',
    created_by      UUID NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (base <> quote),
    UNIQUE (base, quote, effective_date)
);

CREATE INDEX IF NOT EXISTS idx_fx_rates_pair_date ON fx_rates (base, quote, effective_date DESC);

-- Snapshot columns are written once on INSERT; payments_ledger_guard below
-- makes them immutable. All four are NULL when no rate was available.
ALTER TABLE payments
  ADD COLUMN IF NOT EXISTS amount_reporting   BIGINT NULL,
  ADD COLUMN IF NOT EXISTS reporting_currency TEXT NULL,
  ADD COLUMN IF NOT EXISTS fx_rate_scaled     BIGINT NULL CHECK (fx_rate_scaled > 0),
  ADD COLUMN IF NOT EXISTS fx_effective_date  DATE NULL,
  ADD COLUMN IF NOT EXISTS received_at        DATE NULL;

ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_fx_snapshot_check;
ALTER TABLE payments ADD CONSTRAINT payments_fx_snapshot_check CHECK (
  (amount_reporting IS NULL AND reporting_currency IS NULL AND fx_rate_scaled IS NULL AND fx_effective_date IS NULL)
  OR (amount_reporting IS NOT NULL AND reporting_currency IS NOT NULL AND fx_rate_scaled IS NOT NULL AND fx_effective_date IS NOT NULL)
);

-- Runs before the guard is replaced: the 00025 guard does not know received_at yet.
UPDATE payments SET received_at = created_at::date WHERE received_at IS NULL;
ALTER TABLE payments ALTER COLUMN received_at SET DEFAULT CURRENT_DATE;
ALTER TABLE payments ALTER COLUMN received_at SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_payments_booking_created ON payments (booking_id, created_at);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION payments_ledger_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'payments ledger is append-only: DELETE rejected'
            USING ERRCODE = 'insufficient_privilege';
    END IF;

    IF NEW.id                  IS DISTINCT FROM OLD.id
       OR NEW.booking_id          IS DISTINCT FROM OLD.booking_id
       OR NEW.amount              IS DISTINCT FROM OLD.amount
       OR NEW.currency            IS DISTINCT FROM OLD.currency
       OR NEW.method              IS DISTINCT FROM OLD.method
       OR NEW.reference           IS DISTINCT FROM OLD.reference
       OR NEW.recorded_by         IS DISTINCT FROM OLD.recorded_by
       OR NEW.idempotency_key     IS DISTINCT FROM OLD.idempotency_key
       OR NEW.created_at          IS DISTINCT FROM OLD.created_at
       OR NEW.event_type          IS DISTINCT FROM OLD.event_type
       OR NEW.reverses_payment_id IS DISTINCT FROM OLD.reverses_payment_id
       OR NEW.received_at         IS DISTINCT FROM OLD.received_at
       OR NEW.amount_reporting    IS DISTINCT FROM OLD.amount_reporting
       OR NEW.reporting_currency  IS DISTINCT FROM OLD.reporting_currency
       OR NEW.fx_rate_scaled      IS DISTINCT FROM OLD.fx_rate_scaled
       OR NEW.fx_effective_date   IS DISTINCT FROM OLD.fx_effective_date THEN
        RAISE EXCEPTION 'payments ledger entry % is immutable', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    IF NEW.status IS DISTINCT FROM OLD.status
       AND (OLD.event_type, OLD.status, NEW.status) NOT IN (
            ('charge', 'unverified', 'verified'),
            ('refund', 'pending_approval', 'approved'),
            ('refund', 'pending_approval', 'rejected')
       ) THEN
        RAISE EXCEPTION 'payment % status transition % -> % (%) not allowed',
            OLD.id, OLD.status, NEW.status, OLD.event_type
            USING ERRCODE = 'check_violation';
    END IF;

    IF NEW.status IN ('approved', 'rejected') AND NEW.status IS DISTINCT FROM OLD.status
       AND (NEW.approved_by IS NULL OR NEW.approved_at IS NULL) THEN
        RAISE EXCEPTION 'payment % approval requires approved_by and approved_at', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    IF (OLD.approved_by IS NOT NULL AND NEW.approved_by IS DISTINCT FROM OLD.approved_by)
       OR (OLD.approved_at IS NOT NULL AND NEW.approved_at IS DISTINCT FROM OLD.approved_at)
       OR (OLD.note <> '' AND NEW.note IS DISTINCT FROM OLD.note) THEN
        RAISE EXCEPTION 'payment % approved_by/approved_at/note can only be set once', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Booking reporting snapshot, taken when the booking enters "confirmed".
ALTER TABLE bookings
  ADD COLUMN IF NOT EXISTS total_reporting    BIGINT NULL,
  ADD COLUMN IF NOT EXISTS reporting_currency TEXT NULL,
  ADD COLUMN IF NOT EXISTS fx_rate_scaled     BIGINT NULL CHECK (fx_rate_scaled > 0),
  ADD COLUMN IF NOT EXISTS fx_effective_date  DATE NULL;

CREATE TABLE IF NOT EXISTS payment_promises (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id   UUID NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
    branch_id    UUID NOT NULL REFERENCES branches(id),
    amount       BIGINT NOT NULL CHECK (amount > 0),
    currency     TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    promised_on  DATE NOT NULL,
    note         TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'open'
                 CHECK (status IN ('open','kept','broken','cancelled')),
    task_id      UUID NULL REFERENCES tasks(id) ON DELETE SET NULL,
    created_by   UUID NOT NULL REFERENCES users(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at  TIMESTAMPTZ NULL,
    CHECK ((status = 'open') = (resolved_at IS NULL))
);

CREATE INDEX IF NOT EXISTS idx_payment_promises_booking ON payment_promises (booking_id, promised_on);
CREATE INDEX IF NOT EXISTS idx_payment_promises_open ON payment_promises (promised_on, id) WHERE status = 'open';

-- +goose Down
DROP TABLE IF EXISTS payment_promises;

ALTER TABLE bookings
  DROP COLUMN IF EXISTS fx_effective_date,
  DROP COLUMN IF EXISTS fx_rate_scaled,
  DROP COLUMN IF EXISTS reporting_currency,
  DROP COLUMN IF EXISTS total_reporting;

-- Restores the 00025 guard before its new columns disappear.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION payments_ledger_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'payments ledger is append-only: DELETE rejected'
            USING ERRCODE = 'insufficient_privilege';
    END IF;

    IF NEW.id                  IS DISTINCT FROM OLD.id
       OR NEW.booking_id          IS DISTINCT FROM OLD.booking_id
       OR NEW.amount              IS DISTINCT FROM OLD.amount
       OR NEW.currency            IS DISTINCT FROM OLD.currency
       OR NEW.method              IS DISTINCT FROM OLD.method
       OR NEW.reference           IS DISTINCT FROM OLD.reference
       OR NEW.recorded_by         IS DISTINCT FROM OLD.recorded_by
       OR NEW.idempotency_key     IS DISTINCT FROM OLD.idempotency_key
       OR NEW.created_at          IS DISTINCT FROM OLD.created_at
       OR NEW.event_type          IS DISTINCT FROM OLD.event_type
       OR NEW.reverses_payment_id IS DISTINCT FROM OLD.reverses_payment_id THEN
        RAISE EXCEPTION 'payments ledger entry % is immutable', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    IF NEW.status IS DISTINCT FROM OLD.status
       AND (OLD.event_type, OLD.status, NEW.status) NOT IN (
            ('charge', 'unverified', 'verified'),
            ('refund', 'pending_approval', 'approved'),
            ('refund', 'pending_approval', 'rejected')
       ) THEN
        RAISE EXCEPTION 'payment % status transition % -> % (%) not allowed',
            OLD.id, OLD.status, NEW.status, OLD.event_type
            USING ERRCODE = 'check_violation';
    END IF;

    IF NEW.status IN ('approved', 'rejected') AND NEW.status IS DISTINCT FROM OLD.status
       AND (NEW.approved_by IS NULL OR NEW.approved_at IS NULL) THEN
        RAISE EXCEPTION 'payment % approval requires approved_by and approved_at', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    IF (OLD.approved_by IS NOT NULL AND NEW.approved_by IS DISTINCT FROM OLD.approved_by)
       OR (OLD.approved_at IS NOT NULL AND NEW.approved_at IS DISTINCT FROM OLD.approved_at)
       OR (OLD.note <> '' AND NEW.note IS DISTINCT FROM OLD.note) THEN
        RAISE EXCEPTION 'payment % approved_by/approved_at/note can only be set once', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP INDEX IF EXISTS idx_payments_booking_created;
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_fx_snapshot_check;
ALTER TABLE payments
  DROP COLUMN IF EXISTS received_at,
  DROP COLUMN IF EXISTS fx_effective_date,
  DROP COLUMN IF EXISTS fx_rate_scaled,
  DROP COLUMN IF EXISTS reporting_currency,
  DROP COLUMN IF EXISTS amount_reporting;

DROP TABLE IF EXISTS fx_rates;
