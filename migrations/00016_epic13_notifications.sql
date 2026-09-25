-- +goose Up
-- Epic 13: In-app notifications, escalation grouping, external channel prefs

CREATE TABLE IF NOT EXISTS notifications (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id           UUID NOT NULL REFERENCES branches(id),
    recipient_user_id   UUID NOT NULL REFERENCES users(id),
    kind                TEXT NOT NULL,
    severity            TEXT NOT NULL DEFAULT 'info'
        CHECK (severity IN ('info','warning','critical')),
    title               TEXT NOT NULL,
    body                TEXT NOT NULL DEFAULT '',
    entity_type         TEXT NOT NULL DEFAULT '',
    entity_id           UUID NULL,
    group_key           TEXT NOT NULL DEFAULT '',
    occurrence_count    INT NOT NULL DEFAULT 1,
    status              TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open','acknowledged','resolved')),
    href_hint           TEXT NOT NULL DEFAULT '',
    meta_json           JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acknowledged_at     TIMESTAMPTZ NULL,
    acknowledged_by     UUID NULL REFERENCES users(id),
    resolved_at         TIMESTAMPTZ NULL,
    resolved_by         UUID NULL REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_notifications_recipient_status
    ON notifications(recipient_user_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_branch_kind
    ON notifications(branch_id, kind, status);
CREATE INDEX IF NOT EXISTS idx_notifications_group_open
    ON notifications(recipient_user_id, group_key)
    WHERE status = 'open' AND group_key <> '';

CREATE TABLE IF NOT EXISTS notification_preferences (
    user_id        UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    email_enabled  BOOLEAN NOT NULL DEFAULT FALSE,
    push_enabled   BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS notification_preferences;
DROP TABLE IF EXISTS notifications;
