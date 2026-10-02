-- +goose Up
-- Company logo shown on the company's sign-in page. Small raster images only
-- (validated by content sniffing in the domain), kept apart from the profile row.
CREATE TABLE IF NOT EXISTS company_logos (
    company_id    UUID PRIMARY KEY REFERENCES companies(id) ON DELETE CASCADE,
    content_type  TEXT NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg', 'image/webp')),
    data          BYTEA NOT NULL CHECK (octet_length(data) BETWEEN 1 AND 524288),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS company_logos;
