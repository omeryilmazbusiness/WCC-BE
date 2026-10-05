-- +goose Up
-- Package product sheet: header (type, duration, transport, quota, currency) and the
-- structured spec (hotels, flights, transfers, services, itinerary, rules, costs).

ALTER TABLE packages
    ADD COLUMN IF NOT EXISTS kind           TEXT    NOT NULL DEFAULT 'umrah',
    ADD COLUMN IF NOT EXISTS category       TEXT    NOT NULL DEFAULT 'standard',
    ADD COLUMN IF NOT EXISTS duration_days  INT     NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS transport_mode TEXT    NOT NULL DEFAULT 'flight_scheduled',
    ADD COLUMN IF NOT EXISTS capacity_total INT     NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS sales_open     BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS base_currency  TEXT    NOT NULL DEFAULT 'SAR',
    ADD COLUMN IF NOT EXISTS spec           JSONB   NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE packages
    ADD CONSTRAINT packages_kind_check CHECK (kind IN ('umrah', 'hajj')),
    ADD CONSTRAINT packages_transport_check CHECK (transport_mode IN ('flight_scheduled', 'flight_charter', 'road')),
    ADD CONSTRAINT packages_duration_check CHECK (duration_days BETWEEN 0 AND 60),
    ADD CONSTRAINT packages_capacity_check CHECK (capacity_total >= 0),
    ADD CONSTRAINT packages_spec_object CHECK (jsonb_typeof(spec) = 'object');

CREATE INDEX IF NOT EXISTS idx_packages_branch_kind ON packages(branch_id, kind, category);

-- +goose Down
DROP INDEX IF EXISTS idx_packages_branch_kind;
ALTER TABLE packages
    DROP CONSTRAINT IF EXISTS packages_spec_object,
    DROP CONSTRAINT IF EXISTS packages_capacity_check,
    DROP CONSTRAINT IF EXISTS packages_duration_check,
    DROP CONSTRAINT IF EXISTS packages_transport_check,
    DROP CONSTRAINT IF EXISTS packages_kind_check,
    DROP COLUMN IF EXISTS spec,
    DROP COLUMN IF EXISTS base_currency,
    DROP COLUMN IF EXISTS sales_open,
    DROP COLUMN IF EXISTS capacity_total,
    DROP COLUMN IF EXISTS transport_mode,
    DROP COLUMN IF EXISTS duration_days,
    DROP COLUMN IF EXISTS category,
    DROP COLUMN IF EXISTS kind;
