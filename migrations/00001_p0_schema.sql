-- +goose Up
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE branches (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code        TEXT NOT NULL UNIQUE,
    name_en     TEXT NOT NULL,
    name_ar     TEXT NOT NULL DEFAULT '',
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    full_name       TEXT NOT NULL,
    role            TEXT NOT NULL CHECK (role IN ('gm', 'manager', 'employee')),
    branch_id       UUID NOT NULL REFERENCES branches(id),
    team_id         UUID NULL,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_branch ON users(branch_id);

CREATE TABLE customers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id       UUID NOT NULL REFERENCES branches(id),
    full_name       TEXT NOT NULL,
    full_name_ar    TEXT NOT NULL DEFAULT '',
    phone           TEXT NOT NULL,
    email           TEXT NOT NULL DEFAULT '',
    nationality     TEXT NOT NULL DEFAULT '',
    notes           TEXT NOT NULL DEFAULT '',
    created_by      UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_customers_branch_phone ON customers(branch_id, phone);
CREATE INDEX idx_customers_name ON customers USING gin (to_tsvector('simple', coalesce(full_name,'') || ' ' || coalesce(full_name_ar,'')));

CREATE TABLE packages (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id   UUID NOT NULL REFERENCES branches(id),
    code        TEXT NOT NULL,
    name_en     TEXT NOT NULL,
    name_ar     TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (branch_id, code)
);

CREATE TABLE departures (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    package_id      UUID NOT NULL REFERENCES packages(id),
    code            TEXT NOT NULL,
    depart_date     DATE NOT NULL,
    return_date     DATE NOT NULL,
    capacity_total  INT NOT NULL CHECK (capacity_total >= 0),
    capacity_sold   INT NOT NULL DEFAULT 0 CHECK (capacity_sold >= 0),
    base_price      BIGINT NOT NULL DEFAULT 0,
    currency        TEXT NOT NULL DEFAULT 'USD',
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_departures_package ON departures(package_id, depart_date);

CREATE TABLE leads (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id               UUID NOT NULL REFERENCES branches(id),
    customer_id             UUID NULL REFERENCES customers(id),
    full_name               TEXT NOT NULL,
    phone                   TEXT NOT NULL,
    source                  TEXT NOT NULL DEFAULT '',
    stage                   TEXT NOT NULL CHECK (stage IN ('new','contacted','qualified','proposal','won','lost')),
    owner_id                UUID NOT NULL REFERENCES users(id),
    lost_reason             TEXT NOT NULL DEFAULT '',
    notes                   TEXT NOT NULL DEFAULT '',
    converted_booking_id    UUID NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_leads_owner_stage ON leads(owner_id, stage);
CREATE INDEX idx_leads_branch_stage ON leads(branch_id, stage);

CREATE TABLE lead_stage_history (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lead_id     UUID NOT NULL REFERENCES leads(id),
    from_stage  TEXT NULL,
    to_stage    TEXT NOT NULL,
    changed_by  UUID NOT NULL REFERENCES users(id),
    note        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_lead_stage_history_lead ON lead_stage_history(lead_id, created_at);

CREATE TABLE bookings (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id       UUID NOT NULL REFERENCES branches(id),
    customer_id     UUID NOT NULL REFERENCES customers(id),
    departure_id    UUID NOT NULL REFERENCES departures(id),
    lead_id         UUID NULL REFERENCES leads(id),
    status          TEXT NOT NULL CHECK (status IN ('draft','confirmed','cancelled','completed')),
    pax_count       INT NOT NULL CHECK (pax_count > 0),
    total_amount    BIGINT NOT NULL DEFAULT 0,
    collected_amt   BIGINT NOT NULL DEFAULT 0,
    balance_amt     BIGINT NOT NULL DEFAULT 0,
    currency        TEXT NOT NULL DEFAULT 'USD',
    owner_id        UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_bookings_departure_status ON bookings(departure_id, status);
CREATE INDEX idx_bookings_customer ON bookings(customer_id);

CREATE TABLE booking_participants (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id      UUID NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
    full_name       TEXT NOT NULL,
    passport_no     TEXT NOT NULL DEFAULT '',
    nationality     TEXT NOT NULL DEFAULT '',
    date_of_birth   DATE NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Immutable payment ledger (append-only)
CREATE TABLE payments (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id       UUID NOT NULL REFERENCES bookings(id),
    amount           BIGINT NOT NULL CHECK (amount > 0),
    currency         TEXT NOT NULL DEFAULT 'USD',
    method           TEXT NOT NULL DEFAULT '',
    reference        TEXT NOT NULL DEFAULT '',
    recorded_by      UUID NOT NULL REFERENCES users(id),
    idempotency_key  TEXT NOT NULL UNIQUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_payments_booking ON payments(booking_id);

CREATE TABLE tasks (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id        UUID NOT NULL REFERENCES branches(id),
    title            TEXT NOT NULL,
    kind             TEXT NOT NULL CHECK (kind IN ('followup','document','payment','custom')),
    status           TEXT NOT NULL CHECK (status IN ('open','in_progress','done','cancelled')),
    assignee_id      UUID NOT NULL REFERENCES users(id),
    related_type     TEXT NOT NULL,
    related_id       UUID NOT NULL,
    due_at           TIMESTAMPTZ NULL,
    idempotency_key  TEXT UNIQUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at     TIMESTAMPTZ NULL
);

CREATE INDEX idx_tasks_assignee_status ON tasks(assignee_id, status);
CREATE INDEX idx_tasks_related ON tasks(related_type, related_id);

CREATE TABLE documents (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id       UUID NOT NULL REFERENCES branches(id),
    related_type    TEXT NOT NULL,
    related_id      UUID NOT NULL,
    kind            TEXT NOT NULL DEFAULT 'other',
    file_name       TEXT NOT NULL,
    content_type    TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    storage_key     TEXT NOT NULL,
    uploaded_by     UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_documents_related ON documents(related_type, related_id);

CREATE TABLE audit_events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id     UUID NULL REFERENCES users(id),
    action       TEXT NOT NULL,
    entity_type  TEXT NOT NULL,
    entity_id    UUID NULL,
    branch_id    UUID NULL,
    metadata     JSONB NOT NULL DEFAULT '{}'::jsonb,
    ip           TEXT NOT NULL DEFAULT '',
    user_agent   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_events_entity ON audit_events(entity_type, entity_id);
CREATE INDEX idx_audit_events_actor ON audit_events(actor_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS documents;
DROP TABLE IF EXISTS tasks;
DROP TABLE IF EXISTS payments;
DROP TABLE IF EXISTS booking_participants;
DROP TABLE IF EXISTS bookings;
DROP TABLE IF EXISTS lead_stage_history;
DROP TABLE IF EXISTS leads;
DROP TABLE IF EXISTS departures;
DROP TABLE IF EXISTS packages;
DROP TABLE IF EXISTS customers;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS branches;
