-- +goose Up
-- First-login welcome: NULL until the user has been through the welcome screen once.
ALTER TABLE user_preferences ADD COLUMN IF NOT EXISTS welcome_seen_at TIMESTAMPTZ NULL;

-- Anyone who signed in before the welcome existed has already had their first login.
INSERT INTO user_preferences (user_id, welcome_seen_at, updated_at)
SELECT user_id, MIN(created_at), NOW()
FROM auth_sessions
GROUP BY user_id
ON CONFLICT (user_id) DO UPDATE SET
    welcome_seen_at = COALESCE(user_preferences.welcome_seen_at, EXCLUDED.welcome_seen_at);

-- +goose Down
ALTER TABLE user_preferences DROP COLUMN IF EXISTS welcome_seen_at;
