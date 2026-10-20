-- +goose Up
-- Company country is a display name (or a legacy ISO 3166-1 alpha-2 code).
ALTER TABLE companies DROP CONSTRAINT IF EXISTS companies_country_check;
ALTER TABLE companies ADD CONSTRAINT companies_country_len
    CHECK (country = '' OR char_length(btrim(country)) BETWEEN 2 AND 80);

-- +goose Down
UPDATE companies SET country = '' WHERE country <> '' AND country !~ '^[A-Z]{2}$';
ALTER TABLE companies DROP CONSTRAINT IF EXISTS companies_country_len;
ALTER TABLE companies ADD CONSTRAINT companies_country_check
    CHECK (country = '' OR country ~ '^[A-Z]{2}$');
