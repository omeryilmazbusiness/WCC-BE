-- +goose Up
-- Task importance moves to a three-level scale (critical / major / minor) and
-- manual tasks may stand alone, without a linked lead, booking or conversation.
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_priority_check;
UPDATE tasks SET priority = CASE priority
        WHEN 'urgent' THEN 'critical'
        WHEN 'high'   THEN 'major'
        ELSE 'minor'
    END
WHERE priority NOT IN ('critical', 'major', 'minor');
ALTER TABLE tasks ALTER COLUMN priority SET DEFAULT 'minor';
ALTER TABLE tasks ADD CONSTRAINT tasks_priority_check
    CHECK (priority IN ('critical', 'major', 'minor'));

ALTER TABLE tasks ALTER COLUMN related_type DROP NOT NULL;
ALTER TABLE tasks ALTER COLUMN related_id DROP NOT NULL;
ALTER TABLE tasks ADD CONSTRAINT tasks_related_pair_check
    CHECK ((related_type IS NULL) = (related_id IS NULL));

-- +goose Down
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_related_pair_check;
UPDATE tasks SET related_type = 'manual', related_id = id WHERE related_id IS NULL;
ALTER TABLE tasks ALTER COLUMN related_id SET NOT NULL;
ALTER TABLE tasks ALTER COLUMN related_type SET NOT NULL;

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_priority_check;
UPDATE tasks SET priority = CASE priority
        WHEN 'critical' THEN 'urgent'
        WHEN 'major'    THEN 'high'
        ELSE 'normal'
    END;
ALTER TABLE tasks ALTER COLUMN priority SET DEFAULT 'normal';
ALTER TABLE tasks ADD CONSTRAINT tasks_priority_check
    CHECK (priority IN ('low', 'normal', 'high', 'urgent'));
