-- +goose Up
-- Epic 4: pricing tiers, capacity alerts, sales close, immutability support

ALTER TABLE departures
    ADD COLUMN IF NOT EXISTS sales_closed BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS soft_threshold_pct INT NOT NULL DEFAULT 80
        CHECK (soft_threshold_pct >= 0 AND soft_threshold_pct <= 100),
    ADD COLUMN IF NOT EXISTS allow_oversell BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE departures
    DROP CONSTRAINT IF EXISTS departures_capacity_sold_check;
ALTER TABLE departures
    ADD CONSTRAINT departures_capacity_nonneg CHECK (capacity_sold >= 0);

CREATE UNIQUE INDEX IF NOT EXISTS idx_departures_package_code
    ON departures(package_id, code);

CREATE TABLE IF NOT EXISTS package_pricing_tiers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    package_id  UUID NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
    code        TEXT NOT NULL,
    label       TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL CHECK (kind IN ('room', 'occupancy', 'age')),
    amount      BIGINT NOT NULL DEFAULT 0,
    currency    TEXT NOT NULL DEFAULT 'USD',
    sort_order  INT NOT NULL DEFAULT 0,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (package_id, code)
);

CREATE INDEX IF NOT EXISTS idx_pkg_tiers_package ON package_pricing_tiers(package_id, sort_order);

-- Snapshot of tiers at departure create/clone — immutable once capacity_sold > 0 (app-enforced)
CREATE TABLE IF NOT EXISTS departure_pricing_tiers (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    departure_id  UUID NOT NULL REFERENCES departures(id) ON DELETE CASCADE,
    code          TEXT NOT NULL,
    label         TEXT NOT NULL DEFAULT '',
    kind          TEXT NOT NULL CHECK (kind IN ('room', 'occupancy', 'age')),
    amount        BIGINT NOT NULL DEFAULT 0,
    currency      TEXT NOT NULL DEFAULT 'USD',
    sort_order    INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (departure_id, code)
);

CREATE INDEX IF NOT EXISTS idx_dep_tiers_departure ON departure_pricing_tiers(departure_id, sort_order);

-- +goose Down
DROP TABLE IF EXISTS departure_pricing_tiers;
DROP TABLE IF EXISTS package_pricing_tiers;
DROP INDEX IF EXISTS idx_departures_package_code;
ALTER TABLE departures DROP COLUMN IF EXISTS sales_closed;
ALTER TABLE departures DROP COLUMN IF EXISTS soft_threshold_pct;
ALTER TABLE departures DROP COLUMN IF EXISTS allow_oversell;
