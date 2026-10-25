-- +goose Up
-- Commercial WCC package on each tenant. Existing companies keep full access
-- (ultimate); new ones are assigned at platform registration.

ALTER TABLE companies
    ADD COLUMN IF NOT EXISTS plan TEXT NOT NULL DEFAULT 'ultimate'
        CONSTRAINT companies_plan_check CHECK (plan IN ('standard', 'premium', 'ultimate'));

-- +goose Down
ALTER TABLE companies DROP CONSTRAINT IF EXISTS companies_plan_check;
ALTER TABLE companies DROP COLUMN IF EXISTS plan;
