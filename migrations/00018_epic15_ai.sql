-- +goose Up
-- Epic 15: AI Intelligence Layer — BYO provider keys + run audit

CREATE TABLE IF NOT EXISTS ai_settings (
    branch_id           UUID PRIMARY KEY REFERENCES branches(id),
    provider            TEXT NOT NULL DEFAULT ''
        CHECK (provider IN ('','openai','anthropic','gemini')),
    model               TEXT NOT NULL DEFAULT '',
    enabled             BOOLEAN NOT NULL DEFAULT FALSE,
    config_json         JSONB NOT NULL DEFAULT '{}'::jsonb,
    setup_completed_at  TIMESTAMPTZ NULL,
    updated_by          UUID NULL REFERENCES users(id),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ai_runs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id     UUID NOT NULL REFERENCES branches(id),
    actor_id      UUID NULL REFERENCES users(id),
    kind          TEXT NOT NULL,
    provider      TEXT NOT NULL DEFAULT '',
    model         TEXT NOT NULL DEFAULT '',
    scope_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    input_hash    TEXT NOT NULL DEFAULT '',
    output_json   JSONB NOT NULL DEFAULT '{}'::jsonb,
    feedback      TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'ok'
        CHECK (status IN ('ok','error','skipped')),
    error_message TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_runs_branch_kind
    ON ai_runs(branch_id, kind, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_runs_actor
    ON ai_runs(actor_id, created_at DESC) WHERE actor_id IS NOT NULL;

-- Soft lead priority cache (deterministic score; AI may add explanation)
ALTER TABLE leads
    ADD COLUMN IF NOT EXISTS priority_score INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS priority_band TEXT NOT NULL DEFAULT 'normal'
        CHECK (priority_band IN ('low','normal','high','urgent')),
    ADD COLUMN IF NOT EXISTS priority_signals JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS priority_updated_at TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS idx_leads_priority
    ON leads(branch_id, priority_band, priority_score DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_leads_priority;
ALTER TABLE leads DROP COLUMN IF EXISTS priority_updated_at;
ALTER TABLE leads DROP COLUMN IF EXISTS priority_signals;
ALTER TABLE leads DROP COLUMN IF EXISTS priority_band;
ALTER TABLE leads DROP COLUMN IF EXISTS priority_score;
DROP TABLE IF EXISTS ai_runs;
DROP TABLE IF EXISTS ai_settings;
