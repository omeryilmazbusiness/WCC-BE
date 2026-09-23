-- +goose Up
-- Epic 6: Tasks & Workflow — priority, outcome, escalation

ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS priority TEXT NOT NULL DEFAULT 'normal',
    ADD COLUMN IF NOT EXISTS outcome TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS escalated_at TIMESTAMPTZ NULL;

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_priority_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_priority_check
    CHECK (priority IN ('low','normal','high','urgent'));

CREATE INDEX IF NOT EXISTS idx_tasks_branch_status ON tasks(branch_id, status);
CREATE INDEX IF NOT EXISTS idx_tasks_due ON tasks(due_at) WHERE status IN ('open','in_progress');
CREATE INDEX IF NOT EXISTS idx_tasks_escalated ON tasks(escalated_at) WHERE escalated_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_tasks_escalated;
DROP INDEX IF EXISTS idx_tasks_due;
DROP INDEX IF EXISTS idx_tasks_branch_status;
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_priority_check;
ALTER TABLE tasks DROP COLUMN IF EXISTS escalated_at;
ALTER TABLE tasks DROP COLUMN IF EXISTS outcome;
ALTER TABLE tasks DROP COLUMN IF EXISTS priority;
