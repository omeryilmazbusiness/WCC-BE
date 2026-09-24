-- +goose Up
-- Epic 12: Documents lifecycle, visa cases, suppliers, readiness override

ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS review_note TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reviewed_by UUID NULL REFERENCES users(id),
    ADD COLUMN IF NOT EXISTS reviewed_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS expires_at DATE NULL,
    ADD COLUMN IF NOT EXISTS replaces_id UUID NULL REFERENCES documents(id),
    ADD COLUMN IF NOT EXISTS participant_id UUID NULL,
    ADD COLUMN IF NOT EXISTS version INT NOT NULL DEFAULT 1;

-- Backfill status from size_bytes for existing rows
UPDATE documents SET status = 'uploaded' WHERE size_bytes > 0 AND status = 'pending';

ALTER TABLE documents DROP CONSTRAINT IF EXISTS documents_status_check;
ALTER TABLE documents
    ADD CONSTRAINT documents_status_check
    CHECK (status IN ('pending','uploaded','submitted','approved','rejected','expired'));

CREATE INDEX IF NOT EXISTS idx_documents_status ON documents(status);
CREATE INDEX IF NOT EXISTS idx_documents_expires_at ON documents(expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_documents_participant ON documents(participant_id) WHERE participant_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS document_policies (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id    UUID NOT NULL REFERENCES branches(id),
    name         TEXT NOT NULL,
    package_id   UUID NULL REFERENCES packages(id),
    nationality  TEXT NOT NULL DEFAULT '',
    is_active    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_document_policies_branch
    ON document_policies(branch_id, is_active);

CREATE TABLE IF NOT EXISTS document_policy_requirements (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id  UUID NOT NULL REFERENCES document_policies(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    required   BOOLEAN NOT NULL DEFAULT TRUE,
    label      TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_document_policy_reqs_policy
    ON document_policy_requirements(policy_id);

CREATE TABLE IF NOT EXISTS visa_cases (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id       UUID NOT NULL REFERENCES branches(id),
    booking_id      UUID NOT NULL REFERENCES bookings(id),
    participant_id  UUID NULL,
    customer_id     UUID NULL REFERENCES customers(id),
    status          TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft','submitted','processing','approved','rejected','issued','cancelled')),
    external_ref    TEXT NOT NULL DEFAULT '',
    notes           TEXT NOT NULL DEFAULT '',
    submitted_at    TIMESTAMPTZ NULL,
    decided_at      TIMESTAMPTZ NULL,
    expires_at      DATE NULL,
    created_by      UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_visa_cases_booking ON visa_cases(booking_id);
CREATE INDEX IF NOT EXISTS idx_visa_cases_branch_status ON visa_cases(branch_id, status);

CREATE TABLE IF NOT EXISTS visa_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    visa_case_id  UUID NOT NULL REFERENCES visa_cases(id) ON DELETE CASCADE,
    from_status   TEXT NOT NULL,
    to_status     TEXT NOT NULL,
    actor_id      UUID NULL REFERENCES users(id),
    note          TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_visa_events_case ON visa_events(visa_case_id, created_at);

CREATE TABLE IF NOT EXISTS suppliers (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id      UUID NOT NULL REFERENCES branches(id),
    code           TEXT NOT NULL,
    name_en        TEXT NOT NULL DEFAULT '',
    name_ar        TEXT NOT NULL DEFAULT '',
    contact_name   TEXT NOT NULL DEFAULT '',
    contact_phone  TEXT NOT NULL DEFAULT '',
    contact_email  TEXT NOT NULL DEFAULT '',
    terms          TEXT NOT NULL DEFAULT '',
    is_active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (branch_id, code)
);

CREATE INDEX IF NOT EXISTS idx_suppliers_branch ON suppliers(branch_id, is_active);

CREATE TABLE IF NOT EXISTS supplier_links (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    supplier_id          UUID NOT NULL REFERENCES suppliers(id) ON DELETE CASCADE,
    link_type            TEXT NOT NULL CHECK (link_type IN ('package','departure','service')),
    link_id              UUID NOT NULL,
    confirmation_status  TEXT NOT NULL DEFAULT 'pending'
        CHECK (confirmation_status IN ('pending','confirmed','cancelled')),
    confirmation_ref     TEXT NOT NULL DEFAULT '',
    confirmed_at         TIMESTAMPTZ NULL,
    allotment            INT NOT NULL DEFAULT 0,
    sold                 INT NOT NULL DEFAULT 0,
    unit_cost            BIGINT NOT NULL DEFAULT 0,
    currency             TEXT NOT NULL DEFAULT 'SAR',
    notes                TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_supplier_links_supplier ON supplier_links(supplier_id);
CREATE INDEX IF NOT EXISTS idx_supplier_links_target ON supplier_links(link_type, link_id);
CREATE INDEX IF NOT EXISTS idx_supplier_links_status ON supplier_links(confirmation_status);

CREATE TABLE IF NOT EXISTS booking_readiness_overrides (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id  UUID NOT NULL UNIQUE REFERENCES bookings(id) ON DELETE CASCADE,
    reason      TEXT NOT NULL,
    actor_id    UUID NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed default document policy for HQ branch
INSERT INTO document_policies (id, branch_id, name, package_id, nationality, is_active, created_at)
VALUES (
    'a1111111-1111-1111-1111-111111111111',
    '11111111-1111-1111-1111-111111111111',
    'Default travel documents',
    NULL,
    '',
    TRUE,
    NOW()
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO document_policy_requirements (id, policy_id, kind, required, label)
VALUES
    ('a1111111-1111-1111-1111-111111111112', 'a1111111-1111-1111-1111-111111111111', 'passport', TRUE, 'Passport copy'),
    ('a1111111-1111-1111-1111-111111111113', 'a1111111-1111-1111-1111-111111111111', 'visa', TRUE, 'Visa / entry permit'),
    ('a1111111-1111-1111-1111-111111111114', 'a1111111-1111-1111-1111-111111111111', 'photo', TRUE, 'Passport photo')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DELETE FROM document_policy_requirements WHERE policy_id = 'a1111111-1111-1111-1111-111111111111';
DELETE FROM document_policies WHERE id = 'a1111111-1111-1111-1111-111111111111';

DROP TABLE IF EXISTS booking_readiness_overrides;
DROP TABLE IF EXISTS supplier_links;
DROP TABLE IF EXISTS suppliers;
DROP TABLE IF EXISTS visa_events;
DROP TABLE IF EXISTS visa_cases;
DROP TABLE IF EXISTS document_policy_requirements;
DROP TABLE IF EXISTS document_policies;

DROP INDEX IF EXISTS idx_documents_participant;
DROP INDEX IF EXISTS idx_documents_expires_at;
DROP INDEX IF EXISTS idx_documents_status;

ALTER TABLE documents DROP CONSTRAINT IF EXISTS documents_status_check;
ALTER TABLE documents
    DROP COLUMN IF EXISTS version,
    DROP COLUMN IF EXISTS participant_id,
    DROP COLUMN IF EXISTS replaces_id,
    DROP COLUMN IF EXISTS expires_at,
    DROP COLUMN IF EXISTS reviewed_at,
    DROP COLUMN IF EXISTS reviewed_by,
    DROP COLUMN IF EXISTS review_note,
    DROP COLUMN IF EXISTS status;
