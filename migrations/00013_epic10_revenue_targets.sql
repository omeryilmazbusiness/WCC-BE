-- +goose Up
-- Epic 10: Revenue Target & Performance Engine

ALTER TABLE revenue_targets
  ADD COLUMN IF NOT EXISTS metric TEXT NOT NULL DEFAULT 'collected',
  ADD COLUMN IF NOT EXISTS scope_type TEXT NOT NULL DEFAULT 'branch',
  ADD COLUMN IF NOT EXISTS team_id UUID NULL,
  ADD COLUMN IF NOT EXISTS curve_type TEXT NOT NULL DEFAULT 'linear',
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS created_by UUID NULL REFERENCES users(id);

ALTER TABLE revenue_targets DROP CONSTRAINT IF EXISTS revenue_targets_metric_check;
ALTER TABLE revenue_targets ADD CONSTRAINT revenue_targets_metric_check
  CHECK (metric IN ('collected','booked'));

ALTER TABLE revenue_targets DROP CONSTRAINT IF EXISTS revenue_targets_scope_check;
ALTER TABLE revenue_targets ADD CONSTRAINT revenue_targets_scope_check
  CHECK (scope_type IN ('branch','team','employee'));

ALTER TABLE revenue_targets DROP CONSTRAINT IF EXISTS revenue_targets_curve_check;
ALTER TABLE revenue_targets ADD CONSTRAINT revenue_targets_curve_check
  CHECK (curve_type IN ('linear','seasonal'));

-- Monthly (or custom bucket) weights in basis points; must sum to 10000 when seasonal.
CREATE TABLE IF NOT EXISTS revenue_target_weights (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_id  UUID NOT NULL REFERENCES revenue_targets(id) ON DELETE CASCADE,
    bucket     INT NOT NULL CHECK (bucket >= 0 AND bucket < 24),
    weight_bps INT NOT NULL CHECK (weight_bps >= 0 AND weight_bps <= 10000),
    UNIQUE (target_id, bucket)
);

CREATE TABLE IF NOT EXISTS revenue_target_shares (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_id  UUID NOT NULL REFERENCES revenue_targets(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id),
    share_bps  INT NOT NULL CHECK (share_bps >= 0 AND share_bps <= 10000),
    UNIQUE (target_id, user_id)
);

CREATE TABLE IF NOT EXISTS revenue_target_snapshots (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_id        UUID NOT NULL REFERENCES revenue_targets(id) ON DELETE CASCADE,
    as_of            DATE NOT NULL,
    actual_amount    BIGINT NOT NULL DEFAULT 0,
    expected_to_date BIGINT NOT NULL DEFAULT 0,
    variance         BIGINT NOT NULL DEFAULT 0,
    progress_bps     INT NOT NULL DEFAULT 0,
    pace_bps         INT NOT NULL DEFAULT 0,
    forecast_amount  BIGINT NOT NULL DEFAULT 0,
    status           TEXT NOT NULL DEFAULT 'placeholder',
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (target_id, as_of)
);

CREATE TABLE IF NOT EXISTS revenue_target_revisions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_id  UUID NOT NULL REFERENCES revenue_targets(id) ON DELETE CASCADE,
    actor_id   UUID NOT NULL REFERENCES users(id),
    action     TEXT NOT NULL DEFAULT 'revise',
    before_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    after_json  JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_rt_weights_target ON revenue_target_weights(target_id);
CREATE INDEX IF NOT EXISTS idx_rt_shares_target ON revenue_target_shares(target_id);
CREATE INDEX IF NOT EXISTS idx_rt_snapshots_target ON revenue_target_snapshots(target_id, as_of DESC);
CREATE INDEX IF NOT EXISTS idx_rt_revisions_target ON revenue_target_revisions(target_id, created_at DESC);

UPDATE revenue_targets SET metric='collected', scope_type=CASE WHEN owner_id IS NULL THEN 'branch' ELSE 'employee' END,
  curve_type='linear', updated_at=NOW()
WHERE metric IS NULL OR metric = '';

-- +goose Down
DROP TABLE IF EXISTS revenue_target_revisions;
DROP TABLE IF EXISTS revenue_target_snapshots;
DROP TABLE IF EXISTS revenue_target_shares;
DROP TABLE IF EXISTS revenue_target_weights;
ALTER TABLE revenue_targets DROP CONSTRAINT IF EXISTS revenue_targets_metric_check;
ALTER TABLE revenue_targets DROP CONSTRAINT IF EXISTS revenue_targets_scope_check;
ALTER TABLE revenue_targets DROP CONSTRAINT IF EXISTS revenue_targets_curve_check;
ALTER TABLE revenue_targets DROP COLUMN IF EXISTS metric;
ALTER TABLE revenue_targets DROP COLUMN IF EXISTS scope_type;
ALTER TABLE revenue_targets DROP COLUMN IF EXISTS team_id;
ALTER TABLE revenue_targets DROP COLUMN IF EXISTS curve_type;
ALTER TABLE revenue_targets DROP COLUMN IF EXISTS updated_at;
ALTER TABLE revenue_targets DROP COLUMN IF EXISTS created_by;
