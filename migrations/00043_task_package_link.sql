-- +goose Up
-- A task may point at a catalogue package (and one of its departures) next to
-- its related record, so a lead or booking task can carry the trip it is about.
ALTER TABLE tasks
    ADD COLUMN package_id   UUID REFERENCES packages(id) ON DELETE SET NULL,
    ADD COLUMN departure_id UUID REFERENCES departures(id) ON DELETE SET NULL;
ALTER TABLE tasks ADD CONSTRAINT tasks_departure_needs_package_check
    CHECK (departure_id IS NULL OR package_id IS NOT NULL);
CREATE INDEX idx_tasks_package ON tasks (package_id, status) WHERE package_id IS NOT NULL;
CREATE INDEX idx_tasks_departure ON tasks (departure_id) WHERE departure_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_tasks_departure;
DROP INDEX IF EXISTS idx_tasks_package;
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_departure_needs_package_check;
ALTER TABLE tasks DROP COLUMN IF EXISTS departure_id, DROP COLUMN IF EXISTS package_id;
