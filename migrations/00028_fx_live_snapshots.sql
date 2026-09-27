-- +goose Up
-- Live exchange rate board: latest snapshot per (source, kind). payload holds
-- normalized quotes as decimal strings; a failed fetch sets ok=false and error
-- but keeps payload and fetched_at of the last successful fetch.

CREATE TABLE IF NOT EXISTS fx_live_snapshots (
    source          TEXT NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('official', 'market', 'reference')),
    payload         JSONB NOT NULL DEFAULT '{"quotes":[]}'::jsonb,
    fetched_at      TIMESTAMPTZ NULL,
    next_update_at  TIMESTAMPTZ NULL,
    ok              BOOLEAN NOT NULL DEFAULT FALSE,
    error           TEXT NOT NULL DEFAULT '',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (source, kind)
);

-- +goose Down
DROP TABLE IF EXISTS fx_live_snapshots;
