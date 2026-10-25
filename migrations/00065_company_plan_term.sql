-- +goose Up
-- Every commercial package is a fixed 1-year window. Existing tenants start
-- the year from the day the company was created.

ALTER TABLE companies
    ADD COLUMN IF NOT EXISTS plan_started_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

UPDATE companies SET plan_started_at = created_at;

-- +goose Down
ALTER TABLE companies DROP COLUMN IF EXISTS plan_started_at;
