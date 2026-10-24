-- +goose Up
-- Desk idle lock: 0 = off, otherwise 5–120 minutes of no click/type/scroll.
ALTER TABLE companies
    ADD COLUMN idle_logout_minutes INTEGER NOT NULL DEFAULT 15
        CONSTRAINT companies_idle_logout_minutes_check
        CHECK (idle_logout_minutes = 0 OR idle_logout_minutes BETWEEN 5 AND 120);

-- +goose Down
ALTER TABLE companies DROP CONSTRAINT IF EXISTS companies_idle_logout_minutes_check;
ALTER TABLE companies DROP COLUMN IF EXISTS idle_logout_minutes;
