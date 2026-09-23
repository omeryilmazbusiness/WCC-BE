-- +goose Up
-- Epic 8: Unified Inbox + Integrations + SLA

CREATE TABLE IF NOT EXISTS integration_accounts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id     UUID NOT NULL REFERENCES branches(id),
    provider      TEXT NOT NULL CHECK (provider IN ('whatsapp','instagram','email','stub')),
    display_name  TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'ok' CHECK (status IN ('ok','degraded','down')),
    config_json   JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_ok_at    TIMESTAMPTZ,
    last_error    TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (branch_id, provider)
);

CREATE TABLE IF NOT EXISTS channel_identities (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id        UUID NOT NULL REFERENCES branches(id),
    provider         TEXT NOT NULL CHECK (provider IN ('whatsapp','instagram','email','stub')),
    external_key     TEXT NOT NULL,
    display_name     TEXT NOT NULL DEFAULT '',
    phone            TEXT NOT NULL DEFAULT '',
    email            TEXT NOT NULL DEFAULT '',
    customer_id      UUID REFERENCES customers(id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (branch_id, provider, external_key)
);

CREATE TABLE IF NOT EXISTS conversations (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id               UUID NOT NULL REFERENCES branches(id),
    channel                 TEXT NOT NULL CHECK (channel IN ('whatsapp','instagram','email','stub')),
    integration_account_id  UUID REFERENCES integration_accounts(id),
    channel_identity_id     UUID REFERENCES channel_identities(id),
    customer_id             UUID REFERENCES customers(id),
    lead_id                 UUID REFERENCES leads(id),
    owner_id                UUID REFERENCES users(id),
    subject                 TEXT NOT NULL DEFAULT '',
    status                  TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open','resolved','spam','duplicate')),
    sla_started_at          TIMESTAMPTZ,
    sla_due_at              TIMESTAMPTZ,
    sla_breached_at         TIMESTAMPTZ,
    sla_stopped_at          TIMESTAMPTZ,
    last_inbound_at         TIMESTAMPTZ,
    last_outbound_at        TIMESTAMPTZ,
    unanswered_since        TIMESTAMPTZ,
    last_message_preview    TEXT NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_conversations_branch_status ON conversations(branch_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_conversations_owner ON conversations(owner_id) WHERE owner_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_conversations_sla ON conversations(sla_due_at)
    WHERE status = 'open' AND sla_breached_at IS NULL AND sla_stopped_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_conversations_unanswered ON conversations(unanswered_since)
    WHERE status = 'open' AND unanswered_since IS NOT NULL;

CREATE TABLE IF NOT EXISTS messages (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id      UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    branch_id            UUID NOT NULL REFERENCES branches(id),
    direction            TEXT NOT NULL CHECK (direction IN ('in','out','note')),
    body                 TEXT NOT NULL DEFAULT '',
    content_type         TEXT NOT NULL DEFAULT 'text/plain',
    provider             TEXT NOT NULL DEFAULT 'stub',
    provider_message_id  TEXT,
    provider_event_id    TEXT,
    status               TEXT NOT NULL DEFAULT 'received'
        CHECK (status IN ('received','queued','sent','failed','noted')),
    document_id          UUID,
    author_user_id       UUID REFERENCES users(id),
    error_message        TEXT NOT NULL DEFAULT '',
    raw_payload          JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_messages_provider_event
    ON messages(provider, provider_event_id) WHERE provider_event_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_messages_provider_msg
    ON messages(provider, provider_message_id) WHERE provider_message_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages(conversation_id, created_at);

-- Default SLA: first response within 15 minutes for all channels at HQ.
CREATE TABLE IF NOT EXISTS sla_policies (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id                UUID NOT NULL REFERENCES branches(id),
    channel                  TEXT NOT NULL CHECK (channel IN ('whatsapp','instagram','email','stub','*')),
    first_response_seconds   INT NOT NULL DEFAULT 900 CHECK (first_response_seconds > 0),
    UNIQUE (branch_id, channel)
);

INSERT INTO integration_accounts (id, branch_id, provider, display_name, status, last_ok_at)
VALUES
    ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaa01', '11111111-1111-1111-1111-111111111111',
     'stub', 'Stub Channel', 'ok', NOW()),
    ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaa02', '11111111-1111-1111-1111-111111111111',
     'whatsapp', 'WhatsApp (stub mode)', 'ok', NOW()),
    ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaa03', '11111111-1111-1111-1111-111111111111',
     'instagram', 'Instagram (stub mode)', 'degraded', NOW() - INTERVAL '1 hour'),
    ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaa04', '11111111-1111-1111-1111-111111111111',
     'email', 'Email (stub mode)', 'ok', NOW())
ON CONFLICT (branch_id, provider) DO NOTHING;

UPDATE integration_accounts
SET last_error = 'Meta rate limit (demo)', updated_at = NOW()
WHERE id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaa03';

INSERT INTO sla_policies (branch_id, channel, first_response_seconds)
VALUES
    ('11111111-1111-1111-1111-111111111111', '*', 900),
    ('11111111-1111-1111-1111-111111111111', 'whatsapp', 900),
    ('11111111-1111-1111-1111-111111111111', 'instagram', 600),
    ('11111111-1111-1111-1111-111111111111', 'email', 3600)
ON CONFLICT (branch_id, channel) DO NOTHING;

-- Demo open conversation for sales inbox UX (unassigned + one owned).
INSERT INTO channel_identities (id, branch_id, provider, external_key, display_name, phone, email)
VALUES
    ('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01', '11111111-1111-1111-1111-111111111111',
     'whatsapp', 'wa:+905551112233', 'Demo Guest', '+905551112233', ''),
    ('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb02', '11111111-1111-1111-1111-111111111111',
     'email', 'email:guest@example.com', 'Email Guest', '', 'guest@example.com')
ON CONFLICT DO NOTHING;

INSERT INTO conversations (
    id, branch_id, channel, integration_account_id, channel_identity_id,
    owner_id, subject, status, sla_started_at, sla_due_at,
    last_inbound_at, unanswered_since, last_message_preview
) VALUES
    ('cccccccc-cccc-cccc-cccc-cccccccccc01', '11111111-1111-1111-1111-111111111111',
     'whatsapp', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaa02', 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb01',
     NULL, 'Umrah inquiry', 'open',
     NOW() - INTERVAL '20 minutes', NOW() - INTERVAL '5 minutes',
     NOW() - INTERVAL '20 minutes', NOW() - INTERVAL '20 minutes',
     'Do you have April packages?'),
    ('cccccccc-cccc-cccc-cccc-cccccccccc02', '11111111-1111-1111-1111-111111111111',
     'email', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaa04', 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbb02',
     '22222222-2222-2222-2222-222222222203', 'Document question', 'open',
     NOW() - INTERVAL '10 minutes', NOW() + INTERVAL '50 minutes',
     NOW() - INTERVAL '10 minutes', NOW() - INTERVAL '10 minutes',
     'Which documents do I need?')
ON CONFLICT DO NOTHING;

UPDATE conversations
SET sla_breached_at = NOW() - INTERVAL '5 minutes'
WHERE id = 'cccccccc-cccc-cccc-cccc-cccccccccc01' AND sla_breached_at IS NULL;

INSERT INTO messages (id, conversation_id, branch_id, direction, body, provider, provider_event_id, provider_message_id, status, created_at)
VALUES
    ('dddddddd-dddd-dddd-dddd-dddddddddd01', 'cccccccc-cccc-cccc-cccc-cccccccccc01',
     '11111111-1111-1111-1111-111111111111', 'in', 'Do you have April packages?',
     'whatsapp', 'evt-demo-1', 'msg-demo-1', 'received', NOW() - INTERVAL '20 minutes'),
    ('dddddddd-dddd-dddd-dddd-dddddddddd02', 'cccccccc-cccc-cccc-cccc-cccccccccc02',
     '11111111-1111-1111-1111-111111111111', 'in', 'Which documents do I need?',
     'email', 'evt-demo-2', 'msg-demo-2', 'received', NOW() - INTERVAL '10 minutes')
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS conversations;
DROP TABLE IF EXISTS channel_identities;
DROP TABLE IF EXISTS sla_policies;
DROP TABLE IF EXISTS integration_accounts;
