-- +goose Up
-- Year-scale lead archive search without an external engine: Postgres keeps a
-- lowercase search document and a digits-only phone next to each lead, and
-- trigram indexes serve "contains" searches over them.
ALTER TABLE leads
    ADD COLUMN IF NOT EXISTS search_text TEXT GENERATED ALWAYS AS (
        lower(full_name || ' ' || email || ' ' || company_name || ' ' || source)
    ) STORED,
    ADD COLUMN IF NOT EXISTS phone_digits TEXT GENERATED ALWAYS AS (
        regexp_replace(phone, '[^0-9]', '', 'g')
    ) STORED;

-- pg_trgm ships with Postgres contrib and is trusted since PG13. Without it
-- search still works (sequential scan), so its absence never blocks a deploy.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'pg_trgm') THEN
        CREATE EXTENSION IF NOT EXISTS pg_trgm;
        CREATE INDEX IF NOT EXISTS idx_leads_live_search_trgm
            ON leads USING gin (search_text gin_trgm_ops) WHERE deleted_at IS NULL;
        CREATE INDEX IF NOT EXISTS idx_leads_live_phone_trgm
            ON leads USING gin (phone_digits gin_trgm_ops) WHERE deleted_at IS NULL;
    ELSE
        RAISE NOTICE 'pg_trgm not available; lead search runs without trigram indexes';
    END IF;
EXCEPTION WHEN insufficient_privilege THEN
    RAISE NOTICE 'no privilege to create pg_trgm; lead search runs without trigram indexes';
END $$;
-- +goose StatementEnd

-- Loss analysis over the archive: lost leads of a branch by reason, newest first.
CREATE INDEX IF NOT EXISTS idx_leads_live_lost_reason
    ON leads (branch_id, lost_reason_code, updated_at DESC) WHERE deleted_at IS NULL AND stage = 'lost';

-- Owner queues across the archive period filters.
CREATE INDEX IF NOT EXISTS idx_leads_live_owner_created
    ON leads (owner_id, created_at DESC) WHERE deleted_at IS NULL;

-- Head-office views span every branch: newest-first pages and per-stage
-- board lanes walk these instead of sorting the whole year.
CREATE INDEX IF NOT EXISTS idx_leads_live_updated
    ON leads (updated_at DESC, id DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_leads_live_stage_updated_all
    ON leads (stage, updated_at DESC, id DESC) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_leads_live_stage_updated_all;
DROP INDEX IF EXISTS idx_leads_live_updated;
DROP INDEX IF EXISTS idx_leads_live_owner_created;
DROP INDEX IF EXISTS idx_leads_live_lost_reason;
DROP INDEX IF EXISTS idx_leads_live_phone_trgm;
DROP INDEX IF EXISTS idx_leads_live_search_trgm;
ALTER TABLE leads
    DROP COLUMN IF EXISTS phone_digits,
    DROP COLUMN IF EXISTS search_text;
