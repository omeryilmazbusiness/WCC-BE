-- +goose Up
-- Tasks carry an optional free-text description next to the short title.
ALTER TABLE tasks ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD CONSTRAINT tasks_description_length_check
    CHECK (char_length(description) <= 4000);

-- +goose Down
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_description_length_check;
ALTER TABLE tasks DROP COLUMN IF EXISTS description;
