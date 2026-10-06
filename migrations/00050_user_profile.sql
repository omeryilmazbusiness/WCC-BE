-- +goose Up
-- Self-service profile: contact details on the user and an optional photo kept apart from the hot users row.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS phone TEXT,
    ADD COLUMN IF NOT EXISTS job_title TEXT;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_phone_len;
ALTER TABLE users ADD CONSTRAINT users_phone_len CHECK (phone IS NULL OR char_length(phone) <= 32);
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_job_title_len;
ALTER TABLE users ADD CONSTRAINT users_job_title_len CHECK (job_title IS NULL OR char_length(job_title) <= 80);

CREATE TABLE IF NOT EXISTS user_avatars (
    user_id      UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    content_type TEXT NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg', 'image/webp')),
    data         BYTEA NOT NULL CHECK (octet_length(data) <= 1048576),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS user_avatars;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_job_title_len;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_phone_len;
ALTER TABLE users DROP COLUMN IF EXISTS job_title;
ALTER TABLE users DROP COLUMN IF EXISTS phone;
