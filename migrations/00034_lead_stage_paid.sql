-- +goose Up
-- "paid" sits between proposal and won: the customer has paid, the booking is not created yet.
ALTER TABLE leads DROP CONSTRAINT IF EXISTS leads_stage_check;
ALTER TABLE leads ADD CONSTRAINT leads_stage_check
    CHECK (stage IN ('new','contacted','qualified','proposal','paid','won','lost'));

-- +goose Down
UPDATE leads SET stage = 'proposal' WHERE stage = 'paid';
ALTER TABLE leads DROP CONSTRAINT IF EXISTS leads_stage_check;
ALTER TABLE leads ADD CONSTRAINT leads_stage_check
    CHECK (stage IN ('new','contacted','qualified','proposal','won','lost'));
