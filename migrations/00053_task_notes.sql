-- +goose Up
-- Notes between a task's assignee and the manager who created it.
CREATE TABLE IF NOT EXISTS task_notes (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id      UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    branch_id    UUID NOT NULL REFERENCES branches(id),
    author_id    UUID NOT NULL REFERENCES users(id),
    recipient_id UUID NOT NULL REFERENCES users(id),
    body         TEXT NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    read_at      TIMESTAMPTZ NULL,
    CHECK (author_id <> recipient_id)
);

CREATE INDEX IF NOT EXISTS idx_task_notes_task_created ON task_notes (task_id, created_at);
CREATE INDEX IF NOT EXISTS idx_task_notes_recipient_unread ON task_notes (recipient_id, task_id) WHERE read_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS task_notes;
