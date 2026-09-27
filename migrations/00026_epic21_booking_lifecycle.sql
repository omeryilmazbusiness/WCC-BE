-- +goose Up
-- Epic 21: nine-status booking lifecycle (T-268/T-269/T-271) and line-item kinds (T-274)

ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_status_check;

ALTER TABLE bookings
    ADD COLUMN IF NOT EXISTS hold_expires_at   TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS status_changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS status_reason     TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS ready_forced      BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS tax_amt           BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS fee_amt           BIGINT NOT NULL DEFAULT 0;

UPDATE bookings SET status_changed_at = updated_at;

-- Legacy confirmed bookings with money collected are partially paid; the
-- hourly booking.recompute_sweep promotes eligible ones to ready.
WITH moved AS (
    UPDATE bookings SET status = 'partially_paid', status_reason = 'migrated'
    WHERE status = 'confirmed' AND collected_amt > 0
    RETURNING id, branch_id, collected_amt, balance_amt
)
INSERT INTO audit_events (actor_type, action, entity_type, entity_id, branch_id, before, after, metadata)
SELECT 'system', 'booking.status_changed', 'booking', id, branch_id,
       jsonb_build_object('status', 'confirmed'),
       jsonb_build_object('status', 'partially_paid'),
       jsonb_build_object('migration', '00026_epic21_booking_lifecycle', 'actor_kind', 'system',
                          'collected_amt', collected_amt, 'balance_amt', balance_amt)
FROM moved;

ALTER TABLE bookings ADD CONSTRAINT bookings_status_check CHECK (status IN (
    'draft','quoted','option_hold','confirmed','partially_paid','ready','travelled','completed','cancelled'));
ALTER TABLE bookings ADD CONSTRAINT bookings_hold_expiry_check
    CHECK ((status = 'option_hold') = (hold_expires_at IS NOT NULL));
ALTER TABLE bookings ADD CONSTRAINT bookings_tax_fee_check CHECK (tax_amt >= 0 AND fee_amt >= 0);

CREATE INDEX IF NOT EXISTS idx_bookings_hold_expiry ON bookings(hold_expires_at) WHERE status = 'option_hold';

-- Completed bookings now hold seats too; resync stored sold counts.
WITH calc AS (
    SELECT d.id, d.capacity_sold AS sold_before,
           COALESCE(SUM(b.pax_count) FILTER (WHERE b.status IN
               ('option_hold','confirmed','partially_paid','ready','travelled','completed')), 0)::int AS sold_after
    FROM departures d LEFT JOIN bookings b ON b.departure_id = d.id
    GROUP BY d.id
), changed AS (
    UPDATE departures d SET capacity_sold = c.sold_after, updated_at = NOW()
    FROM calc c WHERE d.id = c.id AND d.capacity_sold <> c.sold_after
    RETURNING d.id, c.sold_before, c.sold_after, d.capacity_total
)
INSERT INTO audit_events (actor_type, action, entity_type, entity_id, before, after, metadata)
SELECT 'system', 'departure.capacity_recomputed', 'departure', id,
       jsonb_build_object('capacity_sold', sold_before),
       jsonb_build_object('capacity_sold', sold_after, 'capacity_total', capacity_total),
       jsonb_build_object('migration', '00026_epic21_booking_lifecycle')
FROM changed;

-- The financial summary reads booking_line_items.kind; the former product
-- kind moves to category.
ALTER TABLE booking_line_items DROP CONSTRAINT IF EXISTS booking_line_items_kind_check;
ALTER TABLE booking_line_items RENAME COLUMN kind TO category;
ALTER TABLE booking_line_items ALTER COLUMN category SET DEFAULT '';
ALTER TABLE booking_line_items ADD COLUMN kind TEXT NOT NULL DEFAULT 'item';
ALTER TABLE booking_line_items ADD CONSTRAINT booking_line_items_kind_check
    CHECK (kind IN ('item','tax','fee'));
ALTER TABLE booking_line_items ADD CONSTRAINT booking_line_items_category_check
    CHECK (category IN ('','package','hotel','room','transport','flight','extras'));
ALTER TABLE booking_line_items ADD CONSTRAINT booking_line_items_kind_category_check
    CHECK ((kind = 'item') = (category <> ''));
ALTER TABLE booking_line_items ADD CONSTRAINT booking_line_items_amounts_check
    CHECK (unit_price >= 0 AND unit_cost >= 0);

CREATE INDEX IF NOT EXISTS idx_booking_lines_kind ON booking_line_items(booking_id, kind);

-- +goose Down
-- Lossy: statuses fold back into the four legacy values and tax/fee lines
-- become 'extras' items (booking totals are unchanged).
DROP INDEX IF EXISTS idx_booking_lines_kind;
ALTER TABLE booking_line_items DROP CONSTRAINT IF EXISTS booking_line_items_amounts_check;
ALTER TABLE booking_line_items DROP CONSTRAINT IF EXISTS booking_line_items_kind_category_check;
ALTER TABLE booking_line_items DROP CONSTRAINT IF EXISTS booking_line_items_category_check;
ALTER TABLE booking_line_items DROP CONSTRAINT IF EXISTS booking_line_items_kind_check;
UPDATE booking_line_items SET category = 'extras' WHERE kind <> 'item';
ALTER TABLE booking_line_items DROP COLUMN kind;
ALTER TABLE booking_line_items ALTER COLUMN category DROP DEFAULT;
ALTER TABLE booking_line_items RENAME COLUMN category TO kind;
ALTER TABLE booking_line_items ADD CONSTRAINT booking_line_items_kind_check
    CHECK (kind IN ('package','hotel','room','transport','flight','extras'));

DROP INDEX IF EXISTS idx_bookings_hold_expiry;
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_tax_fee_check;
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_hold_expiry_check;
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_status_check;
UPDATE bookings SET status = CASE status
    WHEN 'quoted' THEN 'draft'
    WHEN 'option_hold' THEN 'draft'
    WHEN 'partially_paid' THEN 'confirmed'
    WHEN 'ready' THEN 'confirmed'
    WHEN 'travelled' THEN 'confirmed'
    ELSE status END
WHERE status IN ('quoted','option_hold','partially_paid','ready','travelled');
ALTER TABLE bookings ADD CONSTRAINT bookings_status_check
    CHECK (status IN ('draft','confirmed','cancelled','completed'));

UPDATE departures d SET capacity_sold = COALESCE((
    SELECT SUM(b.pax_count) FROM bookings b WHERE b.departure_id = d.id AND b.status = 'confirmed'), 0)::int;

ALTER TABLE bookings
    DROP COLUMN IF EXISTS fee_amt,
    DROP COLUMN IF EXISTS tax_amt,
    DROP COLUMN IF EXISTS ready_forced,
    DROP COLUMN IF EXISTS status_reason,
    DROP COLUMN IF EXISTS status_changed_at,
    DROP COLUMN IF EXISTS hold_expires_at;
