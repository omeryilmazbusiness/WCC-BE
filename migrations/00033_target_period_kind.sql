-- +goose Up
-- How a target's range was chosen: calendar week/month/year, a selling season
-- or any custom range. Dates stay the source of truth; the kind drives UI and
-- the canonical boundaries the service enforces.
ALTER TABLE revenue_targets
    ADD COLUMN period_kind TEXT NOT NULL DEFAULT 'custom'
        CONSTRAINT revenue_targets_period_kind_check
        CHECK (period_kind IN ('weekly', 'monthly', 'season', 'yearly', 'custom'));

UPDATE revenue_targets SET period_kind = CASE
    WHEN period_start = date_trunc('year', period_start)::date
     AND period_end = (date_trunc('year', period_start) + INTERVAL '1 year - 1 day')::date THEN 'yearly'
    WHEN period_start = date_trunc('month', period_start)::date
     AND period_end = (date_trunc('month', period_start) + INTERVAL '1 month - 1 day')::date THEN 'monthly'
    WHEN EXTRACT(ISODOW FROM period_start) = 1 AND period_end = period_start + 6 THEN 'weekly'
    WHEN label ILIKE '%season%' THEN 'season'
    ELSE 'custom'
END;

-- +goose Down
ALTER TABLE revenue_targets DROP COLUMN IF EXISTS period_kind;
