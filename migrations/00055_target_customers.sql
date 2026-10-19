-- +goose Up
-- Optional customer-volume goal alongside the money target. Actuals are sold
-- booking pax (travellers sent), not CRM customer rows.
ALTER TABLE revenue_targets
    ADD COLUMN IF NOT EXISTS target_customers INT NULL
        CHECK (target_customers IS NULL OR (target_customers > 0 AND target_customers <= 1000000));

-- +goose Down
ALTER TABLE revenue_targets DROP COLUMN IF EXISTS target_customers;
