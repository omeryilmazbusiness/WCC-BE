-- +goose Up
-- In-app assistant: model answers per user per day, for the soft daily quota and usage stats.
-- Only counters are kept here; prompts and replies are audited in ai_runs.
CREATE TABLE IF NOT EXISTS assistant_usage (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day        DATE NOT NULL,
    branch_id  UUID NOT NULL,
    llm_calls  INTEGER NOT NULL DEFAULT 0 CHECK (llm_calls >= 0),
    tokens_est INTEGER NOT NULL DEFAULT 0 CHECK (tokens_est >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, day)
);

CREATE INDEX IF NOT EXISTS idx_assistant_usage_branch_day ON assistant_usage(branch_id, day);

-- +goose Down
DROP TABLE IF EXISTS assistant_usage;
