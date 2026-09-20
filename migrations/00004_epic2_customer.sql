-- +goose Up
-- Epic 2: Customer 360 fields, companions, merge support

ALTER TABLE customers
    ADD COLUMN IF NOT EXISTS passport_no TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS date_of_birth DATE NULL,
    ADD COLUMN IF NOT EXISTS preferences JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS special_requirements TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS merged_into_id UUID NULL REFERENCES customers(id),
    ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;

CREATE INDEX IF NOT EXISTS idx_customers_passport ON customers(branch_id, passport_no)
    WHERE passport_no <> '';
CREATE INDEX IF NOT EXISTS idx_customers_email ON customers(branch_id, lower(email))
    WHERE email <> '';
CREATE INDEX IF NOT EXISTS idx_customers_merged ON customers(merged_into_id)
    WHERE merged_into_id IS NOT NULL;

CREATE TABLE customer_companions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    companion_id    UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    relation        TEXT NOT NULL DEFAULT 'family', -- family | spouse | child | friend | other
    notes           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (customer_id, companion_id),
    CHECK (customer_id <> companion_id)
);

CREATE INDEX idx_companions_customer ON customer_companions(customer_id);
CREATE INDEX idx_companions_companion ON customer_companions(companion_id);

-- +goose Down
DROP TABLE IF EXISTS customer_companions;
ALTER TABLE customers DROP COLUMN IF EXISTS passport_no;
ALTER TABLE customers DROP COLUMN IF EXISTS date_of_birth;
ALTER TABLE customers DROP COLUMN IF EXISTS preferences;
ALTER TABLE customers DROP COLUMN IF EXISTS special_requirements;
ALTER TABLE customers DROP COLUMN IF EXISTS merged_into_id;
ALTER TABLE customers DROP COLUMN IF EXISTS is_active;
DROP INDEX IF EXISTS idx_customers_passport;
DROP INDEX IF EXISTS idx_customers_email;
DROP INDEX IF EXISTS idx_customers_merged;
