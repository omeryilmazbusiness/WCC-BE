-- +goose Up
-- Epic 3: CRM / Lead pipeline extras

ALTER TABLE leads
    ADD COLUMN IF NOT EXISTS no_follow_up BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS lost_reason_code TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_leads_no_follow_up
    ON leads(branch_id, no_follow_up)
    WHERE no_follow_up = TRUE AND stage NOT IN ('won', 'lost');

CREATE INDEX IF NOT EXISTS idx_leads_source ON leads(branch_id, source);

-- +goose Down
DROP INDEX IF EXISTS idx_leads_no_follow_up;
DROP INDEX IF EXISTS idx_leads_source;
ALTER TABLE leads DROP COLUMN IF EXISTS no_follow_up;
ALTER TABLE leads DROP COLUMN IF EXISTS lost_reason_code;
