-- +goose Up
-- Hotels & contracting: hotel master data, seasonal net rate matrices,
-- child and cancellation policies, allotments and stop sales.

CREATE TABLE IF NOT EXISTS hotels (
    id                  UUID PRIMARY KEY,
    branch_id           UUID NOT NULL REFERENCES branches(id),
    name                TEXT NOT NULL,
    name_ar             TEXT NOT NULL DEFAULT '',
    stars               SMALLINT NOT NULL DEFAULT 0 CHECK (stars BETWEEN 0 AND 5),
    city                TEXT NOT NULL,
    country             TEXT NOT NULL DEFAULT '',
    district            TEXT NOT NULL DEFAULT '',
    latitude            DOUBLE PRECISION,
    longitude           DOUBLE PRECISION,
    landmark            TEXT NOT NULL DEFAULT '',
    distance_m          INT NOT NULL DEFAULT 0 CHECK (distance_m >= 0),
    sales_name          TEXT NOT NULL DEFAULT '',
    sales_phone         TEXT NOT NULL DEFAULT '',
    sales_email         TEXT NOT NULL DEFAULT '',
    reservations_email  TEXT NOT NULL DEFAULT '',
    room_types          TEXT[] NOT NULL DEFAULT '{}',
    meal_plans          TEXT[] NOT NULL DEFAULT '{}',
    currency            TEXT NOT NULL,
    markup              JSONB NOT NULL DEFAULT '{"kind":"percent","value":0}',
    child_policy        JSONB NOT NULL DEFAULT '{}',
    cancellation        JSONB NOT NULL DEFAULT '{}',
    notes               TEXT NOT NULL DEFAULT '',
    is_active           BOOLEAN NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT hotels_coords_check CHECK ((latitude IS NULL) = (longitude IS NULL))
);

CREATE INDEX IF NOT EXISTS idx_hotels_branch ON hotels(branch_id, is_active, city);
CREATE INDEX IF NOT EXISTS idx_hotels_name ON hotels(branch_id, lower(name));

CREATE TABLE IF NOT EXISTS hotel_seasons (
    id          UUID PRIMARY KEY,
    hotel_id    UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('low','shoulder','high','peak')),
    start_date  DATE NOT NULL,
    end_date    DATE NOT NULL,
    markup      JSONB,
    rates       JSONB NOT NULL DEFAULT '[]',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT hotel_seasons_range_check CHECK (end_date >= start_date)
);

CREATE INDEX IF NOT EXISTS idx_hotel_seasons_hotel ON hotel_seasons(hotel_id, start_date);

CREATE TABLE IF NOT EXISTS hotel_allotments (
    id            UUID PRIMARY KEY,
    hotel_id      UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
    room_type     TEXT NOT NULL,
    kind          TEXT NOT NULL CHECK (kind IN ('guaranteed','on_request')),
    start_date    DATE NOT NULL,
    end_date      DATE NOT NULL,
    rooms         INT NOT NULL CHECK (rooms > 0),
    sold          INT NOT NULL DEFAULT 0,
    release_days  INT NOT NULL DEFAULT 0 CHECK (release_days BETWEEN 0 AND 90),
    notes         TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT hotel_allotments_range_check CHECK (end_date >= start_date),
    CONSTRAINT hotel_allotments_sold_check CHECK (sold BETWEEN 0 AND rooms)
);

CREATE INDEX IF NOT EXISTS idx_hotel_allotments_hotel ON hotel_allotments(hotel_id, start_date);

CREATE TABLE IF NOT EXISTS hotel_stop_sales (
    id          UUID PRIMARY KEY,
    hotel_id    UUID NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
    start_date  DATE NOT NULL,
    end_date    DATE NOT NULL,
    room_type   TEXT NOT NULL DEFAULT '',
    reason      TEXT NOT NULL DEFAULT '',
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT hotel_stop_sales_range_check CHECK (end_date >= start_date)
);

CREATE INDEX IF NOT EXISTS idx_hotel_stop_sales_hotel ON hotel_stop_sales(hotel_id, start_date);

-- +goose Down
DROP TABLE IF EXISTS hotel_stop_sales;
DROP TABLE IF EXISTS hotel_allotments;
DROP TABLE IF EXISTS hotel_seasons;
DROP TABLE IF EXISTS hotels;
