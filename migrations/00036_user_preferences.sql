-- +goose Up
-- Per-user UI preferences. NULL nav_favorites means "use the role default".
CREATE TABLE IF NOT EXISTS user_preferences (
    user_id        UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    nav_favorites  TEXT[] NULL,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT user_preferences_nav_favorites_max CHECK (
        nav_favorites IS NULL OR cardinality(nav_favorites) <= 5
    )
);

-- +goose Down
DROP TABLE IF EXISTS user_preferences;
