-- +goose Up
-- Epic 5: Booking workspace — line items, discount, checklist

ALTER TABLE bookings
    ADD COLUMN IF NOT EXISTS discount_amt BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cost_amt BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS notes TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS booking_line_items (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id  UUID NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('package','hotel','room','transport','flight','extras')),
    label       TEXT NOT NULL DEFAULT '',
    quantity    INT NOT NULL DEFAULT 1 CHECK (quantity > 0),
    unit_price  BIGINT NOT NULL DEFAULT 0,
    unit_cost   BIGINT NOT NULL DEFAULT 0,
    sort_order  INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_booking_lines_booking ON booking_line_items(booking_id, sort_order);

CREATE TABLE IF NOT EXISTS booking_checklist_items (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id   UUID NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
    code         TEXT NOT NULL,
    label        TEXT NOT NULL DEFAULT '',
    required     BOOLEAN NOT NULL DEFAULT TRUE,
    completed    BOOLEAN NOT NULL DEFAULT FALSE,
    completed_at TIMESTAMPTZ NULL,
    sort_order   INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (booking_id, code)
);

CREATE INDEX IF NOT EXISTS idx_booking_checklist ON booking_checklist_items(booking_id, sort_order);

CREATE INDEX IF NOT EXISTS idx_bookings_branch_status ON bookings(branch_id, status);
CREATE INDEX IF NOT EXISTS idx_bookings_customer ON bookings(customer_id);
CREATE INDEX IF NOT EXISTS idx_bookings_departure ON bookings(departure_id);

-- +goose Down
DROP INDEX IF EXISTS idx_bookings_departure;
DROP INDEX IF EXISTS idx_bookings_customer;
DROP INDEX IF EXISTS idx_bookings_branch_status;
DROP TABLE IF EXISTS booking_checklist_items;
DROP TABLE IF EXISTS booking_line_items;
ALTER TABLE bookings DROP COLUMN IF EXISTS discount_amt;
ALTER TABLE bookings DROP COLUMN IF EXISTS cost_amt;
ALTER TABLE bookings DROP COLUMN IF EXISTS notes;
