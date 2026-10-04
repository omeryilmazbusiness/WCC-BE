-- +goose Up
-- Who the customer is (contact, segment, billing identity), how urgent and
-- ready they are, and the full travel scope an agent needs to quote.
ALTER TABLE leads
    ADD COLUMN email             TEXT        NOT NULL DEFAULT '',
    ADD COLUMN segment           TEXT        NOT NULL DEFAULT 'b2c'
        CONSTRAINT leads_segment_valid CHECK (segment IN ('b2c','b2b')),
    ADD COLUMN company_name      TEXT        NOT NULL DEFAULT '',
    ADD COLUMN tax_number        TEXT        NOT NULL DEFAULT '',
    ADD COLUMN tax_office        TEXT        NOT NULL DEFAULT '',
    ADD COLUMN priority          TEXT        NOT NULL DEFAULT 'medium'
        CONSTRAINT leads_priority_valid CHECK (priority IN ('high','medium','low')),
    ADD COLUMN intent            TEXT        NOT NULL DEFAULT ''
        CONSTRAINT leads_intent_valid CHECK (intent IN ('','ready','comparing','planning')),
    ADD COLUMN next_follow_up_at TIMESTAMPTZ,
    ADD COLUMN services          TEXT[]      NOT NULL DEFAULT '{}',
    ADD COLUMN origin            TEXT        NOT NULL DEFAULT '',
    ADD COLUMN destination       TEXT        NOT NULL DEFAULT '',
    ADD COLUMN return_date       DATE,
    ADD COLUMN flex_days         SMALLINT    NOT NULL DEFAULT 0
        CONSTRAINT leads_flex_days_range CHECK (flex_days BETWEEN 0 AND 30),
    ADD COLUMN adults            SMALLINT    NOT NULL DEFAULT 0
        CONSTRAINT leads_adults_non_negative CHECK (adults >= 0),
    ADD COLUMN child_ages        SMALLINT[]  NOT NULL DEFAULT '{}',
    ADD COLUMN infants           SMALLINT    NOT NULL DEFAULT 0
        CONSTRAINT leads_infants_non_negative CHECK (infants >= 0),
    ADD COLUMN cabin_class       TEXT        NOT NULL DEFAULT '',
    ADD COLUMN board_type        TEXT        NOT NULL DEFAULT '',
    ADD COLUMN preferences       TEXT[]      NOT NULL DEFAULT '{}',
    ADD CONSTRAINT leads_return_after_travel CHECK (return_date IS NULL OR travel_date IS NULL OR return_date >= travel_date);

CREATE INDEX idx_leads_follow_up ON leads (next_follow_up_at)
    WHERE next_follow_up_at IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX idx_leads_branch_priority ON leads (branch_id, priority) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_leads_branch_priority;
DROP INDEX IF EXISTS idx_leads_follow_up;
ALTER TABLE leads
    DROP CONSTRAINT IF EXISTS leads_return_after_travel,
    DROP COLUMN IF EXISTS preferences,
    DROP COLUMN IF EXISTS board_type,
    DROP COLUMN IF EXISTS cabin_class,
    DROP COLUMN IF EXISTS infants,
    DROP COLUMN IF EXISTS child_ages,
    DROP COLUMN IF EXISTS adults,
    DROP COLUMN IF EXISTS flex_days,
    DROP COLUMN IF EXISTS return_date,
    DROP COLUMN IF EXISTS destination,
    DROP COLUMN IF EXISTS origin,
    DROP COLUMN IF EXISTS services,
    DROP COLUMN IF EXISTS next_follow_up_at,
    DROP COLUMN IF EXISTS intent,
    DROP COLUMN IF EXISTS priority,
    DROP COLUMN IF EXISTS tax_office,
    DROP COLUMN IF EXISTS tax_number,
    DROP COLUMN IF EXISTS company_name,
    DROP COLUMN IF EXISTS segment,
    DROP COLUMN IF EXISTS email;
