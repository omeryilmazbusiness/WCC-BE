-- +goose Up
-- What the customer asked for: when, how many, how much and which package.
-- Every column is optional; a lead fills in as the conversation goes on.
ALTER TABLE leads
    ADD COLUMN travel_date      DATE,
    ADD COLUMN travel_window    TEXT   NOT NULL DEFAULT '',
    ADD COLUMN pax_count        INT    CONSTRAINT leads_pax_count_range CHECK (pax_count BETWEEN 1 AND 500),
    ADD COLUMN budget_amount    BIGINT CONSTRAINT leads_budget_non_negative CHECK (budget_amount >= 0),
    ADD COLUMN budget_currency  TEXT   NOT NULL DEFAULT '',
    ADD COLUMN package_id       UUID   REFERENCES packages(id) ON DELETE SET NULL,
    ADD COLUMN package_interest TEXT   NOT NULL DEFAULT '';

CREATE INDEX idx_leads_package ON leads (package_id) WHERE package_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_leads_package;
ALTER TABLE leads
    DROP COLUMN IF EXISTS package_interest,
    DROP COLUMN IF EXISTS package_id,
    DROP COLUMN IF EXISTS budget_currency,
    DROP COLUMN IF EXISTS budget_amount,
    DROP COLUMN IF EXISTS pax_count,
    DROP COLUMN IF EXISTS travel_window,
    DROP COLUMN IF EXISTS travel_date;
