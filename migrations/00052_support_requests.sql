-- +goose Up
-- Help requests users send from Settings → Help; platform admins work them from the support inbox.
CREATE TABLE IF NOT EXISTS support_requests (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    number       BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    company_id   UUID NULL REFERENCES companies(id) ON DELETE SET NULL,
    branch_id    UUID NOT NULL REFERENCES branches(id),
    requester_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title        TEXT NOT NULL CHECK (char_length(title) BETWEEN 4 AND 120),
    description  TEXT NOT NULL CHECK (char_length(description) BETWEEN 10 AND 4000),
    status       TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'in_progress', 'resolved')),
    page         TEXT NOT NULL DEFAULT '' CHECK (char_length(page) <= 200),
    locale       TEXT NOT NULL DEFAULT 'en' CHECK (locale IN ('en', 'ar')),
    admin_note   TEXT NOT NULL DEFAULT '' CHECK (char_length(admin_note) <= 1000),
    resolved_at  TIMESTAMPTZ NULL,
    resolved_by  UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_support_requests_status_created ON support_requests (status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_support_requests_requester_created ON support_requests (requester_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS support_requests;
