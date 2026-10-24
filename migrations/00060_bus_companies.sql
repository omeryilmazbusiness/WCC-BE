-- +goose Up
-- Branch-scoped coach operators used on the Operations → Buses desk.
CREATE TABLE bus_companies (
    id            UUID        PRIMARY KEY,
    branch_id     UUID        NOT NULL REFERENCES branches(id),
    name          TEXT        NOT NULL,
    phone         TEXT        NOT NULL DEFAULT '',
    email         TEXT        NOT NULL DEFAULT '',
    website       TEXT        NOT NULL DEFAULT '',
    country       TEXT        NOT NULL DEFAULT '',
    city          TEXT        NOT NULL DEFAULT '',
    address       TEXT        NOT NULL DEFAULT '',
    vehicle_class TEXT        NOT NULL DEFAULT 'standard',
    fleet_size    INT         NOT NULL DEFAULT 0,
    plate_prefix  TEXT        NOT NULL DEFAULT '',
    license_no    TEXT        NOT NULL DEFAULT '',
    contact_name  TEXT        NOT NULL DEFAULT '',
    contact_phone TEXT        NOT NULL DEFAULT '',
    notes         TEXT        NOT NULL DEFAULT '',
    is_active     BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL,
    CONSTRAINT bus_companies_class_check CHECK (vehicle_class IN ('standard','vip','sleeper','mixed')),
    CONSTRAINT bus_companies_fleet_check CHECK (fleet_size >= 0 AND fleet_size <= 5000)
);

CREATE INDEX idx_bus_companies_branch ON bus_companies (branch_id, name);

-- +goose Down
DROP TABLE IF EXISTS bus_companies;
