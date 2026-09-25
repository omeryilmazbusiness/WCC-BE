-- +goose Up
-- Epic 18: %100 Gap Closure — admin config, rooming, search foundations

-- T-221: Escalation rule overlays (merge over code defaults)
CREATE TABLE IF NOT EXISTS escalation_rule_overrides (
    branch_id               UUID NOT NULL REFERENCES branches(id),
    kind                    TEXT NOT NULL,
    escalate_after_seconds  INT NOT NULL DEFAULT 0 CHECK (escalate_after_seconds >= 0),
    escalate_to_roles       TEXT[] NOT NULL DEFAULT '{}',
    enabled                 BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (branch_id, kind)
);

-- T-223: Branch-editable lost reason taxonomy
CREATE TABLE IF NOT EXISTS lost_reason_codes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id       UUID NOT NULL REFERENCES branches(id),
    code            TEXT NOT NULL,
    label_en        TEXT NOT NULL DEFAULT '',
    label_ar        TEXT NOT NULL DEFAULT '',
    sort_order      INT NOT NULL DEFAULT 0,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    requires_note   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (branch_id, code)
);

CREATE INDEX IF NOT EXISTS idx_lost_reason_codes_branch
    ON lost_reason_codes(branch_id, is_active, sort_order);

-- T-224: Inbox message templates / snippets
CREATE TABLE IF NOT EXISTS message_templates (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id       UUID NOT NULL REFERENCES branches(id),
    channel         TEXT NOT NULL DEFAULT '*'
        CHECK (channel IN ('*','whatsapp','instagram','email','stub')),
    name            TEXT NOT NULL,
    body            TEXT NOT NULL DEFAULT '',
    variables_json  JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    created_by      UUID NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_message_templates_branch
    ON message_templates(branch_id, is_active, channel);

-- T-226: Field visibility/requiredness by entity (role-agnostic MVP; role filter later)
CREATE TABLE IF NOT EXISTS entity_field_configs (
    branch_id       UUID NOT NULL REFERENCES branches(id),
    entity          TEXT NOT NULL
        CHECK (entity IN ('customer','lead','booking','payment','task')),
    field_key       TEXT NOT NULL,
    visible         BOOLEAN NOT NULL DEFAULT TRUE,
    required        BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order      INT NOT NULL DEFAULT 0,
    label_override  TEXT NOT NULL DEFAULT '',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (branch_id, entity, field_key)
);

-- T-227: Global alert / threshold settings (one row per branch)
CREATE TABLE IF NOT EXISTS alert_threshold_settings (
    branch_id                   UUID PRIMARY KEY REFERENCES branches(id),
    capacity_soft_pct           INT NOT NULL DEFAULT 80 CHECK (capacity_soft_pct BETWEEN 1 AND 100),
    payment_overdue_hours       INT NOT NULL DEFAULT 12 CHECK (payment_overdue_hours > 0),
    missing_doc_hours           INT NOT NULL DEFAULT 24 CHECK (missing_doc_hours > 0),
    lead_no_followup_hours      INT NOT NULL DEFAULT 24 CHECK (lead_no_followup_hours > 0),
    target_behind_pct           INT NOT NULL DEFAULT 15 CHECK (target_behind_pct BETWEEN 1 AND 100),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- T-228: Rooming / group list
CREATE TABLE IF NOT EXISTS departure_rooms (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    departure_id    UUID NOT NULL REFERENCES departures(id) ON DELETE CASCADE,
    branch_id       UUID NOT NULL REFERENCES branches(id),
    label           TEXT NOT NULL DEFAULT '',
    room_type       TEXT NOT NULL DEFAULT 'double'
        CHECK (room_type IN ('single','double','triple','quad','suite','other')),
    capacity        INT NOT NULL DEFAULT 2 CHECK (capacity > 0),
    notes           TEXT NOT NULL DEFAULT '',
    sort_order      INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_departure_rooms_departure
    ON departure_rooms(departure_id, sort_order);

CREATE TABLE IF NOT EXISTS room_assignments (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id             UUID NOT NULL REFERENCES departure_rooms(id) ON DELETE CASCADE,
    participant_id      UUID NOT NULL REFERENCES booking_participants(id) ON DELETE CASCADE,
    booking_id          UUID NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
    assigned_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    assigned_by         UUID NULL REFERENCES users(id),
    UNIQUE (participant_id)
);

CREATE INDEX IF NOT EXISTS idx_room_assignments_room
    ON room_assignments(room_id);
CREATE INDEX IF NOT EXISTS idx_room_assignments_booking
    ON room_assignments(booking_id);

-- T-235: Supplier issue / performance history
CREATE TABLE IF NOT EXISTS supplier_issue_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id       UUID NOT NULL REFERENCES branches(id),
    supplier_id     UUID NOT NULL REFERENCES suppliers(id) ON DELETE CASCADE,
    link_id         UUID NULL REFERENCES supplier_links(id) ON DELETE SET NULL,
    kind            TEXT NOT NULL DEFAULT 'note'
        CHECK (kind IN ('note','delay','quality','cancellation','invoice','other')),
    severity        TEXT NOT NULL DEFAULT 'info'
        CHECK (severity IN ('info','warning','critical')),
    note            TEXT NOT NULL DEFAULT '',
    actor_id        UUID NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_supplier_issue_events
    ON supplier_issue_events(supplier_id, created_at DESC);

-- Seed default lost reasons for HQ (from hardcoded catalog)
INSERT INTO lost_reason_codes (branch_id, code, label_en, label_ar, sort_order, requires_note)
VALUES
    ('11111111-1111-1111-1111-111111111111', 'price', 'Price', 'السعر', 10, false),
    ('11111111-1111-1111-1111-111111111111', 'timing', 'Timing', 'التوقيت', 20, false),
    ('11111111-1111-1111-1111-111111111111', 'competitor', 'Competitor', 'منافس', 30, false),
    ('11111111-1111-1111-1111-111111111111', 'no_response', 'No response', 'لا رد', 40, false),
    ('11111111-1111-1111-1111-111111111111', 'other', 'Other', 'أخرى', 90, true)
ON CONFLICT (branch_id, code) DO NOTHING;

INSERT INTO alert_threshold_settings (branch_id)
VALUES ('11111111-1111-1111-1111-111111111111')
ON CONFLICT (branch_id) DO NOTHING;

-- +goose Down
DROP INDEX IF EXISTS idx_supplier_issue_events;
DROP TABLE IF EXISTS supplier_issue_events;
DROP INDEX IF EXISTS idx_room_assignments_booking;
DROP INDEX IF EXISTS idx_room_assignments_room;
DROP TABLE IF EXISTS room_assignments;
DROP INDEX IF EXISTS idx_departure_rooms_departure;
DROP TABLE IF EXISTS departure_rooms;
DROP TABLE IF EXISTS alert_threshold_settings;
DROP TABLE IF EXISTS entity_field_configs;
DROP INDEX IF EXISTS idx_message_templates_branch;
DROP TABLE IF EXISTS message_templates;
DROP INDEX IF EXISTS idx_lost_reason_codes_branch;
DROP TABLE IF EXISTS lost_reason_codes;
DROP TABLE IF EXISTS escalation_rule_overrides;
