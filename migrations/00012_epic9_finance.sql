-- +goose Up
-- Epic 9: Finance ledger extensions, schedules, reporting currency

-- Signed ledger amounts (reverse/refund are negative); keep nonzero.
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_amount_check;
ALTER TABLE payments ADD CONSTRAINT payments_amount_check CHECK (amount <> 0);

ALTER TABLE payments
  ADD COLUMN IF NOT EXISTS event_type TEXT NOT NULL DEFAULT 'charge',
  ADD COLUMN IF NOT EXISTS reverses_payment_id UUID NULL REFERENCES payments(id),
  ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'verified',
  ADD COLUMN IF NOT EXISTS approved_by UUID NULL,
  ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS note TEXT NOT NULL DEFAULT '';

ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_event_type_check;
ALTER TABLE payments ADD CONSTRAINT payments_event_type_check
  CHECK (event_type IN ('charge','reverse','adjust','refund'));

ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_status_check;
ALTER TABLE payments ADD CONSTRAINT payments_status_check
  CHECK (status IN ('unverified','verified','pending_approval','approved','rejected'));

CREATE INDEX IF NOT EXISTS idx_payments_booking_status ON payments (booking_id, status);
CREATE INDEX IF NOT EXISTS idx_payments_status ON payments (status);
CREATE INDEX IF NOT EXISTS idx_payments_event_type ON payments (event_type);

CREATE TABLE IF NOT EXISTS payment_schedules (
    id              UUID PRIMARY KEY,
    booking_id      UUID NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
    due_at          TIMESTAMPTZ NOT NULL,
    amount          BIGINT NOT NULL CHECK (amount > 0),
    currency        TEXT NOT NULL DEFAULT 'SAR',
    label           TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'open'
                    CHECK (status IN ('open','paid','cancelled','overdue')),
    reminder_sent_at TIMESTAMPTZ NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_payment_schedules_due
  ON payment_schedules (due_at) WHERE status = 'open';
CREATE INDEX IF NOT EXISTS idx_payment_schedules_booking
  ON payment_schedules (booking_id);

CREATE TABLE IF NOT EXISTS finance_settings (
    branch_id           UUID PRIMARY KEY REFERENCES branches(id),
    reporting_currency  TEXT NOT NULL DEFAULT 'SAR',
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- New charges default to unverified for finance workflow; existing rows stay verified.
UPDATE payments SET status = 'verified' WHERE status IS NULL OR status = '';

-- +goose Down
DROP TABLE IF EXISTS finance_settings;
DROP TABLE IF EXISTS payment_schedules;
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_event_type_check;
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_status_check;
ALTER TABLE payments DROP COLUMN IF EXISTS event_type;
ALTER TABLE payments DROP COLUMN IF EXISTS reverses_payment_id;
ALTER TABLE payments DROP COLUMN IF EXISTS status;
ALTER TABLE payments DROP COLUMN IF EXISTS approved_by;
ALTER TABLE payments DROP COLUMN IF EXISTS approved_at;
ALTER TABLE payments DROP COLUMN IF EXISTS note;
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_amount_check;
ALTER TABLE payments ADD CONSTRAINT payments_amount_check CHECK (amount > 0);
