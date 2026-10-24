-- +goose Up
ALTER TABLE bus_companies DROP COLUMN IF EXISTS fleet_size;
ALTER TABLE bus_companies DROP COLUMN IF EXISTS plate_prefix;

-- +goose Down
ALTER TABLE bus_companies ADD COLUMN IF NOT EXISTS fleet_size INT NOT NULL DEFAULT 0;
ALTER TABLE bus_companies ADD COLUMN IF NOT EXISTS plate_prefix TEXT NOT NULL DEFAULT '';
