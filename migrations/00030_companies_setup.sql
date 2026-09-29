-- +goose Up
-- Companies (tenants) own branches; every user belongs to exactly one branch
-- and therefore to exactly one company. GMs act company-wide from their home
-- branch. Onboarding progress is tracked per company.

CREATE TABLE companies (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        TEXT NOT NULL UNIQUE
                CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(slug) BETWEEN 2 AND 48),
    name_en     TEXT NOT NULL CHECK (length(btrim(name_en)) BETWEEN 2 AND 120),
    name_ar     TEXT NOT NULL DEFAULT '',
    legal_name  TEXT NOT NULL DEFAULT '',
    phone       TEXT NOT NULL DEFAULT '',
    email       TEXT NOT NULL DEFAULT '',
    website     TEXT NOT NULL DEFAULT '',
    country     TEXT NOT NULL DEFAULT '' CHECK (country = '' OR country ~ '^[A-Z]{2}$'),
    city        TEXT NOT NULL DEFAULT '',
    address     TEXT NOT NULL DEFAULT '',
    currency    TEXT NOT NULL DEFAULT 'USD' CHECK (currency ~ '^[A-Z]{3}$'),
    timezone    TEXT NOT NULL DEFAULT 'Asia/Riyadh',
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by  UUID NULL REFERENCES users(id) ON DELETE SET NULL
);

ALTER TABLE branches
    ADD COLUMN company_id UUID NULL REFERENCES companies(id),
    ADD COLUMN slug       TEXT NULL,
    ADD COLUMN kind       TEXT NOT NULL DEFAULT 'branch' CHECK (kind IN ('main_center', 'branch'));

-- Existing installations become one company: the oldest branch is its main center.
-- +goose StatementBegin
DO $$
DECLARE
    first_branch branches%ROWTYPE;
    company UUID;
    company_slug TEXT;
BEGIN
    SELECT * INTO first_branch FROM branches ORDER BY created_at, id LIMIT 1;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    company_slug := btrim(regexp_replace(lower(first_branch.name_en), '[^a-z0-9]+', '-', 'g'), '-');
    IF length(company_slug) < 2 THEN
        company_slug := btrim(regexp_replace(lower(first_branch.code), '[^a-z0-9]+', '-', 'g'), '-');
    END IF;
    IF length(company_slug) < 2 THEN
        company_slug := 'company';
    END IF;
    INSERT INTO companies (slug, name_en, name_ar, timezone)
    VALUES (left(company_slug, 48), first_branch.name_en, first_branch.name_ar, first_branch.timezone)
    RETURNING id INTO company;

    UPDATE branches SET company_id = company;
    UPDATE branches SET kind = 'main_center' WHERE id = first_branch.id;
    UPDATE branches b
    SET slug = s.slug
    FROM (
        SELECT id,
               CASE WHEN row_number() OVER (PARTITION BY base ORDER BY created_at, id) = 1 THEN base
                    ELSE base || '-' || lower(regexp_replace(code, '[^a-zA-Z0-9]+', '', 'g'))
               END AS slug
        FROM (
            SELECT id, code, created_at,
                   COALESCE(NULLIF(btrim(regexp_replace(lower(name_en), '[^a-z0-9]+', '-', 'g'), '-'), ''),
                            lower(regexp_replace(code, '[^a-zA-Z0-9]+', '', 'g'))) AS base
            FROM branches
        ) named
    ) s
    WHERE b.id = s.id;
    -- The company took the oldest branch's name, so that branch becomes "Main Center"
    -- at /main: URLs read /{company}/main, not /{company}/{company}.
    UPDATE branches SET slug = slug || '-' || lower(regexp_replace(code, '[^a-zA-Z0-9]+', '', 'g'))
    WHERE slug = 'main' AND id <> first_branch.id;
    UPDATE branches
    SET slug = 'main', name_en = 'Main Center', name_ar = 'المركز الرئيسي'
    WHERE id = first_branch.id;
END $$;
-- +goose StatementEnd

ALTER TABLE branches
    ALTER COLUMN company_id SET NOT NULL,
    ALTER COLUMN slug SET NOT NULL,
    ADD CONSTRAINT branches_slug_format
        CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(slug) BETWEEN 2 AND 48),
    ADD CONSTRAINT branches_company_slug_key UNIQUE (company_id, slug);

-- A company has at most one main center.
CREATE UNIQUE INDEX branches_one_main_center ON branches (company_id) WHERE kind = 'main_center';
CREATE INDEX idx_branches_company ON branches (company_id);

CREATE TABLE company_setup (
    company_id          UUID PRIMARY KEY REFERENCES companies(id) ON DELETE CASCADE,
    company_done_at     TIMESTAMPTZ NULL,
    staff_done_at       TIMESTAMPTZ NULL,
    ai_skipped_at       TIMESTAMPTZ NULL,
    channels_skipped_at TIMESTAMPTZ NULL,
    dismissed_at        TIMESTAMPTZ NULL,
    completed_at        TIMESTAMPTZ NULL,
    completed_by        UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS company_setup;
DROP INDEX IF EXISTS idx_branches_company;
DROP INDEX IF EXISTS branches_one_main_center;
ALTER TABLE branches
    DROP CONSTRAINT IF EXISTS branches_company_slug_key,
    DROP CONSTRAINT IF EXISTS branches_slug_format,
    DROP COLUMN IF EXISTS kind,
    DROP COLUMN IF EXISTS slug,
    DROP COLUMN IF EXISTS company_id;
DROP TABLE IF EXISTS companies;
