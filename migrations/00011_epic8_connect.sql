-- +goose Up
-- Epic 8 follow-up: multi-channel connect (facebook, gmail) + connection states

ALTER TABLE integration_accounts DROP CONSTRAINT IF EXISTS integration_accounts_provider_check;
ALTER TABLE integration_accounts ADD CONSTRAINT integration_accounts_provider_check
    CHECK (provider IN ('whatsapp','instagram','facebook','gmail','email','stub'));

ALTER TABLE integration_accounts DROP CONSTRAINT IF EXISTS integration_accounts_status_check;
ALTER TABLE integration_accounts ADD CONSTRAINT integration_accounts_status_check
    CHECK (status IN ('disconnected','pending','connected','ok','degraded','down'));

ALTER TABLE channel_identities DROP CONSTRAINT IF EXISTS channel_identities_provider_check;
ALTER TABLE channel_identities ADD CONSTRAINT channel_identities_provider_check
    CHECK (provider IN ('whatsapp','instagram','facebook','gmail','email','stub'));

ALTER TABLE conversations DROP CONSTRAINT IF EXISTS conversations_channel_check;
ALTER TABLE conversations ADD CONSTRAINT conversations_channel_check
    CHECK (channel IN ('whatsapp','instagram','facebook','gmail','email','stub'));

ALTER TABLE sla_policies DROP CONSTRAINT IF EXISTS sla_policies_channel_check;
ALTER TABLE sla_policies ADD CONSTRAINT sla_policies_channel_check
    CHECK (channel IN ('whatsapp','instagram','facebook','gmail','email','stub','*'));

-- Seed disconnected placeholders for new channels (HQ demo branch).
INSERT INTO integration_accounts (branch_id, provider, display_name, status)
SELECT '11111111-1111-1111-1111-111111111111', v.provider, v.display_name, 'disconnected'
FROM (VALUES
    ('facebook', 'Facebook Messenger'),
    ('gmail', 'Gmail')
) AS v(provider, display_name)
WHERE NOT EXISTS (
    SELECT 1 FROM integration_accounts ia
    WHERE ia.branch_id = '11111111-1111-1111-1111-111111111111' AND ia.provider = v.provider
);

-- Existing demo accounts start disconnected until manager connects with credentials.
UPDATE integration_accounts
SET status = 'disconnected', updated_at = NOW()
WHERE branch_id = '11111111-1111-1111-1111-111111111111'
  AND provider IN ('whatsapp','instagram','email','stub','facebook','gmail')
  AND (config_json = '{}'::jsonb OR config_json IS NULL OR NOT (config_json ? 'access_token'));

-- +goose Down
ALTER TABLE integration_accounts DROP CONSTRAINT IF EXISTS integration_accounts_provider_check;
ALTER TABLE integration_accounts ADD CONSTRAINT integration_accounts_provider_check
    CHECK (provider IN ('whatsapp','instagram','email','stub'));
ALTER TABLE integration_accounts DROP CONSTRAINT IF EXISTS integration_accounts_status_check;
ALTER TABLE integration_accounts ADD CONSTRAINT integration_accounts_status_check
    CHECK (status IN ('ok','degraded','down'));
ALTER TABLE channel_identities DROP CONSTRAINT IF EXISTS channel_identities_provider_check;
ALTER TABLE channel_identities ADD CONSTRAINT channel_identities_provider_check
    CHECK (provider IN ('whatsapp','instagram','email','stub'));
ALTER TABLE conversations DROP CONSTRAINT IF EXISTS conversations_channel_check;
ALTER TABLE conversations ADD CONSTRAINT conversations_channel_check
    CHECK (channel IN ('whatsapp','instagram','email','stub'));
ALTER TABLE sla_policies DROP CONSTRAINT IF EXISTS sla_policies_channel_check;
ALTER TABLE sla_policies ADD CONSTRAINT sla_policies_channel_check
    CHECK (channel IN ('whatsapp','instagram','email','stub','*'));
DELETE FROM integration_accounts WHERE provider IN ('facebook','gmail');
