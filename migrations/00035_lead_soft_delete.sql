-- +goose Up
-- Deleted leads stay for undo and audit; every read filters deleted_at IS NULL.
ALTER TABLE leads
    ADD COLUMN deleted_at TIMESTAMPTZ NULL,
    ADD COLUMN deleted_by UUID NULL REFERENCES users(id);

-- Board lanes and paged lists: live leads of a branch per stage, newest activity first.
CREATE INDEX IF NOT EXISTS idx_leads_live_stage_updated
    ON leads(branch_id, stage, updated_at DESC) WHERE deleted_at IS NULL;
-- Period filters (this week / this month).
CREATE INDEX IF NOT EXISTS idx_leads_live_created
    ON leads(branch_id, created_at DESC) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_leads_live_created;
DROP INDEX IF EXISTS idx_leads_live_stage_updated;
ALTER TABLE leads
    DROP COLUMN IF EXISTS deleted_by,
    DROP COLUMN IF EXISTS deleted_at;
