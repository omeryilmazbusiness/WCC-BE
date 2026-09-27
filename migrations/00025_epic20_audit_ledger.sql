-- +goose Up
-- Epic 20: audit correctness (actor model, before/after columns), append-only
-- audit trail and immutable payment ledger.

ALTER TABLE audit_events
    ADD COLUMN IF NOT EXISTS actor_type TEXT NOT NULL DEFAULT 'user',
    ADD COLUMN IF NOT EXISTS session_id UUID NULL,
    ADD COLUMN IF NOT EXISTS request_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS before     JSONB NULL,
    ADD COLUMN IF NOT EXISTS after      JSONB NULL,
    ADD COLUMN IF NOT EXISTS row_hash   TEXT NOT NULL DEFAULT '';

ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_actor_type_check;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_actor_type_check
    CHECK (actor_type IN ('user','system','webhook'));

-- Backfill runs before the append-only trigger exists.
UPDATE audit_events
SET before     = metadata -> 'before',
    after      = metadata -> 'after',
    metadata   = metadata - 'before' - 'after',
    actor_type = CASE
        WHEN action LIKE 'webhook.%' THEN 'webhook'
        WHEN actor_id IS NULL THEN 'system'
        ELSE 'user'
    END;

CREATE INDEX IF NOT EXISTS idx_audit_events_session ON audit_events(session_id) WHERE session_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_audit_events_request ON audit_events(request_id) WHERE request_id <> '';

-- Tamper evidence: row_hash = sha256 over the canonical row content.
-- Verify with: SELECT id FROM audit_events a WHERE row_hash <> audit_event_hash(a);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION audit_event_hash(e audit_events) RETURNS TEXT
LANGUAGE sql STABLE AS $$
    SELECT encode(sha256(convert_to(concat_ws(E'\x1f',
        e.id::text, COALESCE(e.actor_id::text, ''), e.actor_type, e.action, e.entity_type,
        COALESCE(e.entity_id::text, ''), COALESCE(e.branch_id::text, ''),
        COALESCE(e.before::text, ''), COALESCE(e.after::text, ''), e.metadata::text,
        e.ip, e.user_agent, COALESCE(e.session_id::text, ''), e.request_id,
        to_char(e.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US')
    ), 'UTF8')), 'hex')
$$;
-- +goose StatementEnd

UPDATE audit_events a SET row_hash = audit_event_hash(a);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION audit_events_set_hash() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.row_hash := audit_event_hash(NEW);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS audit_events_hash ON audit_events;
CREATE TRIGGER audit_events_hash
    BEFORE INSERT ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_set_hash();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION audit_events_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only: % rejected', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS audit_events_no_modify ON audit_events;
CREATE TRIGGER audit_events_no_modify
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_append_only();

DROP TRIGGER IF EXISTS audit_events_no_truncate ON audit_events;
CREATE TRIGGER audit_events_no_truncate
    BEFORE TRUNCATE ON audit_events
    FOR EACH STATEMENT EXECUTE FUNCTION audit_events_append_only();

REVOKE UPDATE, DELETE, TRUNCATE ON audit_events FROM PUBLIC;

-- The migrating role is the application role; drop its own write-back rights too.
-- +goose StatementBegin
DO $$
BEGIN
    EXECUTE format('REVOKE UPDATE, DELETE, TRUNCATE ON audit_events FROM %I', current_user);
END;
$$;
-- +goose StatementEnd

-- Immutable payment ledger: rows are never deleted and only whitelisted status
-- transitions may change them. Mirrors payment.StatusTransitions (Go).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION payments_ledger_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'payments ledger is append-only: DELETE rejected'
            USING ERRCODE = 'insufficient_privilege';
    END IF;

    IF NEW.id                  IS DISTINCT FROM OLD.id
       OR NEW.booking_id          IS DISTINCT FROM OLD.booking_id
       OR NEW.amount              IS DISTINCT FROM OLD.amount
       OR NEW.currency            IS DISTINCT FROM OLD.currency
       OR NEW.method              IS DISTINCT FROM OLD.method
       OR NEW.reference           IS DISTINCT FROM OLD.reference
       OR NEW.recorded_by         IS DISTINCT FROM OLD.recorded_by
       OR NEW.idempotency_key     IS DISTINCT FROM OLD.idempotency_key
       OR NEW.created_at          IS DISTINCT FROM OLD.created_at
       OR NEW.event_type          IS DISTINCT FROM OLD.event_type
       OR NEW.reverses_payment_id IS DISTINCT FROM OLD.reverses_payment_id THEN
        RAISE EXCEPTION 'payments ledger entry % is immutable', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    IF NEW.status IS DISTINCT FROM OLD.status
       AND (OLD.event_type, OLD.status, NEW.status) NOT IN (
            ('charge', 'unverified', 'verified'),
            ('refund', 'pending_approval', 'approved'),
            ('refund', 'pending_approval', 'rejected')
       ) THEN
        RAISE EXCEPTION 'payment % status transition % -> % (%) not allowed',
            OLD.id, OLD.status, NEW.status, OLD.event_type
            USING ERRCODE = 'check_violation';
    END IF;

    IF NEW.status IN ('approved', 'rejected') AND NEW.status IS DISTINCT FROM OLD.status
       AND (NEW.approved_by IS NULL OR NEW.approved_at IS NULL) THEN
        RAISE EXCEPTION 'payment % approval requires approved_by and approved_at', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    IF (OLD.approved_by IS NOT NULL AND NEW.approved_by IS DISTINCT FROM OLD.approved_by)
       OR (OLD.approved_at IS NOT NULL AND NEW.approved_at IS DISTINCT FROM OLD.approved_at)
       OR (OLD.note <> '' AND NEW.note IS DISTINCT FROM OLD.note) THEN
        RAISE EXCEPTION 'payment % approved_by/approved_at/note can only be set once', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS payments_ledger_immutable ON payments;
CREATE TRIGGER payments_ledger_immutable
    BEFORE UPDATE OR DELETE ON payments
    FOR EACH ROW EXECUTE FUNCTION payments_ledger_guard();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION payments_ledger_no_truncate() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'payments ledger is append-only: TRUNCATE rejected'
        USING ERRCODE = 'insufficient_privilege';
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS payments_ledger_no_truncate ON payments;
CREATE TRIGGER payments_ledger_no_truncate
    BEFORE TRUNCATE ON payments
    FOR EACH STATEMENT EXECUTE FUNCTION payments_ledger_no_truncate();

-- +goose Down
DROP TRIGGER IF EXISTS payments_ledger_no_truncate ON payments;
DROP FUNCTION IF EXISTS payments_ledger_no_truncate();
DROP TRIGGER IF EXISTS payments_ledger_immutable ON payments;
DROP FUNCTION IF EXISTS payments_ledger_guard();

DROP TRIGGER IF EXISTS audit_events_no_truncate ON audit_events;
DROP TRIGGER IF EXISTS audit_events_no_modify ON audit_events;
DROP FUNCTION IF EXISTS audit_events_append_only();
DROP TRIGGER IF EXISTS audit_events_hash ON audit_events;
DROP FUNCTION IF EXISTS audit_events_set_hash();
DROP FUNCTION IF EXISTS audit_event_hash(audit_events);

-- +goose StatementBegin
DO $$
BEGIN
    EXECUTE format('GRANT UPDATE, DELETE, TRUNCATE ON audit_events TO %I', current_user);
END;
$$;
-- +goose StatementEnd

UPDATE audit_events
SET metadata = metadata
    || CASE WHEN before IS NULL THEN '{}'::jsonb ELSE jsonb_build_object('before', before) END
    || CASE WHEN after  IS NULL THEN '{}'::jsonb ELSE jsonb_build_object('after', after) END;

DROP INDEX IF EXISTS idx_audit_events_request;
DROP INDEX IF EXISTS idx_audit_events_session;
ALTER TABLE audit_events DROP CONSTRAINT IF EXISTS audit_events_actor_type_check;
ALTER TABLE audit_events
    DROP COLUMN IF EXISTS row_hash,
    DROP COLUMN IF EXISTS after,
    DROP COLUMN IF EXISTS before,
    DROP COLUMN IF EXISTS request_id,
    DROP COLUMN IF EXISTS session_id,
    DROP COLUMN IF EXISTS actor_type;
