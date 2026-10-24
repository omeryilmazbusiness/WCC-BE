-- +goose Up
-- Custom (other) tours sit next to Umrah / Hajj. Tiers stay in the category column.
ALTER TABLE packages DROP CONSTRAINT IF EXISTS packages_kind_check;
ALTER TABLE packages ADD CONSTRAINT packages_kind_check CHECK (kind IN ('umrah', 'hajj', 'other'));

-- +goose Down
UPDATE packages SET kind = 'umrah' WHERE kind = 'other';
ALTER TABLE packages DROP CONSTRAINT IF EXISTS packages_kind_check;
ALTER TABLE packages ADD CONSTRAINT packages_kind_check CHECK (kind IN ('umrah', 'hajj'));
