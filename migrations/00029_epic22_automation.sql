-- +goose Up
-- Epic 22: durable events (outbox), scheduler bookkeeping, automation columns
-- and realtime fan-out.

-- T-281 transactional outbox: rows are written in the same transaction as the
-- business change and delivered by the worker dispatcher with backoff.
CREATE TABLE IF NOT EXISTS outbox_events (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT NOT NULL,
    payload          JSONB NOT NULL DEFAULT '{}'::jsonb,
    branch_id        UUID NULL,
    status           TEXT NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'processing', 'dispatched', 'dead')),
    attempts         INT NOT NULL DEFAULT 0,
    next_attempt_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_until     TIMESTAMPTZ NULL,
    last_error       TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    dispatched_at    TIMESTAMPTZ NULL
);
CREATE INDEX IF NOT EXISTS outbox_events_due_idx
    ON outbox_events (next_attempt_at) WHERE status IN ('pending', 'processing');
CREATE INDEX IF NOT EXISTS outbox_events_dead_idx
    ON outbox_events (created_at DESC) WHERE status = 'dead';
CREATE INDEX IF NOT EXISTS outbox_events_dispatched_idx
    ON outbox_events (dispatched_at) WHERE status = 'dispatched';

-- Once-per-period ledger for scheduled work (AI summary per branch day,
-- expiry reminders per threshold, scheduled reports per run).
CREATE TABLE IF NOT EXISTS scheduled_job_runs (
    job        TEXT NOT NULL,
    scope_key  TEXT NOT NULL,
    period     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (job, scope_key, period)
);

-- T-286 automatic task provenance.
ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS source_rule TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS created_by UUID NULL REFERENCES users(id),
    ADD COLUMN IF NOT EXISTS overdue_notified_at TIMESTAMPTZ NULL;
CREATE INDEX IF NOT EXISTS tasks_source_rule_idx ON tasks (source_rule) WHERE source_rule <> '';
CREATE INDEX IF NOT EXISTS tasks_overdue_pending_idx
    ON tasks (due_at) WHERE status IN ('open', 'in_progress') AND overdue_notified_at IS NULL;

UPDATE tasks SET source_rule = CASE
    WHEN idempotency_key LIKE 'lead:%:followup' THEN 'lead.follow_up'
    WHEN idempotency_key LIKE 'lead:%:converted-booking' THEN 'lead.converted'
    WHEN idempotency_key LIKE 'booking:%:document' THEN 'booking.documents'
    WHEN idempotency_key LIKE 'booking:%:payment' THEN 'booking.payment'
    WHEN idempotency_key LIKE 'booking:%:payment-due:%' THEN 'payment.due'
    WHEN idempotency_key LIKE 'booking:%:hold-expired:%' THEN 'booking.hold_expired'
    WHEN idempotency_key LIKE 'target:%:recovery:%' THEN 'target.recovery'
    WHEN idempotency_key LIKE 'payment-promise:%:follow-up' THEN 'payment.promise'
    WHEN idempotency_key LIKE 'payment-promise:%:broken' THEN 'payment.promise_broken'
    WHEN idempotency_key LIKE 'conv:%:outcome:%' THEN 'conversation.outcome'
    ELSE ''
END
WHERE idempotency_key IS NOT NULL AND source_rule = '';

-- T-284 SLA A (warning) / B (breach) as a percentage of the policy window.
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS sla_warned_at TIMESTAMPTZ NULL;
ALTER TABLE alert_threshold_settings
    ADD COLUMN IF NOT EXISTS sla_warn_pct INT NOT NULL DEFAULT 75,
    ADD COLUMN IF NOT EXISTS sla_breach_pct INT NOT NULL DEFAULT 100,
    ADD COLUMN IF NOT EXISTS visa_follow_up_days INT NOT NULL DEFAULT 7;
ALTER TABLE alert_threshold_settings
    ADD CONSTRAINT alert_threshold_sla_pct_chk
    CHECK (sla_warn_pct BETWEEN 10 AND 100 AND sla_breach_pct BETWEEN 50 AND 300 AND sla_warn_pct < sla_breach_pct);
ALTER TABLE alert_threshold_settings
    ADD CONSTRAINT alert_threshold_visa_days_chk CHECK (visa_follow_up_days BETWEEN 1 AND 90);

-- T-279 branch-local schedules (AI daily summary at 07:00 local time).
ALTER TABLE branches ADD COLUMN IF NOT EXISTS timezone TEXT NOT NULL DEFAULT 'Asia/Riyadh';

-- T-280 passport expiry reminders.
ALTER TABLE customers ADD COLUMN IF NOT EXISTS passport_expires_at DATE NULL;
CREATE INDEX IF NOT EXISTS customers_passport_expiry_idx
    ON customers (passport_expires_at) WHERE passport_expires_at IS NOT NULL;

-- T-280 webhook retry bookkeeping.
ALTER TABLE webhook_events
    ADD COLUMN IF NOT EXISTS attempts INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ NULL;
CREATE INDEX IF NOT EXISTS webhook_events_retry_idx
    ON webhook_events (next_retry_at) WHERE status = 'failed';

-- T-280 scheduled reports.
CREATE TABLE IF NOT EXISTS report_schedules (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id     UUID NOT NULL REFERENCES branches(id),
    kind          TEXT NOT NULL,
    frequency     TEXT NOT NULL CHECK (frequency IN ('daily', 'weekly', 'monthly')),
    recipient_ids UUID[] NOT NULL DEFAULT '{}',
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    created_by    UUID NULL REFERENCES users(id),
    last_run_at   TIMESTAMPTZ NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS report_schedules_branch_idx ON report_schedules (branch_id) WHERE enabled;

CREATE TABLE IF NOT EXISTS report_runs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    schedule_id  UUID NOT NULL REFERENCES report_schedules(id) ON DELETE CASCADE,
    branch_id    UUID NOT NULL REFERENCES branches(id),
    kind         TEXT NOT NULL,
    period       TEXT NOT NULL,
    filename     TEXT NOT NULL,
    row_count    INT NOT NULL DEFAULT 0,
    content      BYTEA NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (schedule_id, period)
);

-- T-288 realtime: every notification change is announced to API replicas.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION wcc_notify_notification() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('wcc_events', json_build_object(
        'type', 'notification',
        'user_id', NEW.recipient_user_id,
        'branch_id', NEW.branch_id,
        'topic', NEW.kind
    )::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS notifications_realtime ON notifications;
CREATE TRIGGER notifications_realtime
    AFTER INSERT OR UPDATE ON notifications
    FOR EACH ROW EXECUTE FUNCTION wcc_notify_notification();

-- +goose Down
DROP TRIGGER IF EXISTS notifications_realtime ON notifications;
DROP FUNCTION IF EXISTS wcc_notify_notification();
DROP TABLE IF EXISTS report_runs;
DROP TABLE IF EXISTS report_schedules;
DROP INDEX IF EXISTS webhook_events_retry_idx;
ALTER TABLE webhook_events DROP COLUMN IF EXISTS next_retry_at, DROP COLUMN IF EXISTS attempts;
DROP INDEX IF EXISTS customers_passport_expiry_idx;
ALTER TABLE customers DROP COLUMN IF EXISTS passport_expires_at;
ALTER TABLE branches DROP COLUMN IF EXISTS timezone;
ALTER TABLE alert_threshold_settings DROP CONSTRAINT IF EXISTS alert_threshold_visa_days_chk;
ALTER TABLE alert_threshold_settings DROP CONSTRAINT IF EXISTS alert_threshold_sla_pct_chk;
ALTER TABLE alert_threshold_settings
    DROP COLUMN IF EXISTS visa_follow_up_days,
    DROP COLUMN IF EXISTS sla_breach_pct,
    DROP COLUMN IF EXISTS sla_warn_pct;
ALTER TABLE conversations DROP COLUMN IF EXISTS sla_warned_at;
DROP INDEX IF EXISTS tasks_overdue_pending_idx;
DROP INDEX IF EXISTS tasks_source_rule_idx;
ALTER TABLE tasks
    DROP COLUMN IF EXISTS overdue_notified_at,
    DROP COLUMN IF EXISTS created_by,
    DROP COLUMN IF EXISTS source_rule;
DROP TABLE IF EXISTS scheduled_job_runs;
DROP TABLE IF EXISTS outbox_events;
