-- +goose Up
-- Visa product sheets sit next to Umrah / Hajj / other. Types and prices live on spec.visa_offers.
ALTER TABLE packages DROP CONSTRAINT IF EXISTS packages_kind_check;
ALTER TABLE packages ADD CONSTRAINT packages_kind_check CHECK (kind IN ('umrah', 'hajj', 'other', 'visa'));

-- +goose Down
UPDATE packages SET kind = 'other' WHERE kind = 'visa';
ALTER TABLE packages DROP CONSTRAINT IF EXISTS packages_kind_check;
ALTER TABLE packages ADD CONSTRAINT packages_kind_check CHECK (kind IN ('umrah', 'hajj', 'other'));
