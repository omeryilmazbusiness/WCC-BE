-- +goose Up
-- Epic 7: Manager/Employee workspace — revenue targets for progress cards

CREATE TABLE IF NOT EXISTS revenue_targets (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id     UUID NOT NULL REFERENCES branches(id),
    owner_id      UUID NULL REFERENCES users(id),
    label         TEXT NOT NULL DEFAULT 'Season target',
    target_amount BIGINT NOT NULL CHECK (target_amount > 0),
    currency      TEXT NOT NULL DEFAULT 'USD',
    period_start  DATE NOT NULL,
    period_end    DATE NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (period_end >= period_start)
);

CREATE INDEX IF NOT EXISTS idx_revenue_targets_branch ON revenue_targets(branch_id, period_start, period_end);
CREATE INDEX IF NOT EXISTS idx_revenue_targets_owner ON revenue_targets(owner_id) WHERE owner_id IS NOT NULL;

-- Seed branch-level target for current season (demo / first branch).
INSERT INTO revenue_targets (branch_id, owner_id, label, target_amount, currency, period_start, period_end)
SELECT b.id, NULL, 'Season target', 100000000, 'USD',
       DATE_TRUNC('year', CURRENT_DATE)::date,
       (DATE_TRUNC('year', CURRENT_DATE) + INTERVAL '1 year' - INTERVAL '1 day')::date
FROM branches b
WHERE NOT EXISTS (SELECT 1 FROM revenue_targets rt WHERE rt.branch_id = b.id AND rt.owner_id IS NULL)
ORDER BY b.created_at
LIMIT 1;

-- +goose Down
DROP INDEX IF EXISTS idx_revenue_targets_owner;
DROP INDEX IF EXISTS idx_revenue_targets_branch;
DROP TABLE IF EXISTS revenue_targets;
