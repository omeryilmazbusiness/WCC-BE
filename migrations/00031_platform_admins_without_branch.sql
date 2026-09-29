-- +goose Up
-- Platform admins operate the whole platform and belong to no company:
-- they have no branch, every other role has exactly one.
ALTER TABLE users ALTER COLUMN branch_id DROP NOT NULL;
UPDATE users SET branch_id = NULL, team_id = NULL WHERE role = 'admin';
ALTER TABLE users
    ADD CONSTRAINT users_branch_matches_role CHECK ((role = 'admin') = (branch_id IS NULL));

-- +goose Down
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_branch_matches_role;
UPDATE users
SET branch_id = (SELECT id FROM branches WHERE kind = 'main_center' ORDER BY created_at, id LIMIT 1)
WHERE branch_id IS NULL;
ALTER TABLE users ALTER COLUMN branch_id SET NOT NULL;
