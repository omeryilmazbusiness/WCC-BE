-- +goose Up
-- Personal packages sit next to luxury as a category and belong to one customer.
ALTER TABLE packages
    ADD COLUMN IF NOT EXISTS customer_id UUID REFERENCES customers(id);

ALTER TABLE packages
    DROP CONSTRAINT IF EXISTS packages_personal_customer_check;

ALTER TABLE packages
    ADD CONSTRAINT packages_personal_customer_check
    CHECK ((category = 'personal') = (customer_id IS NOT NULL));

CREATE INDEX IF NOT EXISTS idx_packages_personal_customer
    ON packages (branch_id, customer_id)
    WHERE category = 'personal';

-- +goose Down
DROP INDEX IF EXISTS idx_packages_personal_customer;
ALTER TABLE packages DROP CONSTRAINT IF EXISTS packages_personal_customer_check;
ALTER TABLE packages DROP COLUMN IF EXISTS customer_id;
