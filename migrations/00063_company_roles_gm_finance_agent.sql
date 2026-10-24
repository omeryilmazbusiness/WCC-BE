-- +goose Up
-- Company staff titles collapse to gm / finance / agent. Platform admin is unchanged.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
UPDATE users SET role = 'agent' WHERE role IN ('employee', 'operations', 'manager');
ALTER TABLE users ADD CONSTRAINT users_role_check
    CHECK (role IN ('gm', 'finance', 'agent', 'admin'));

-- +goose Down
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
    CHECK (role IN ('gm', 'manager', 'employee', 'finance', 'operations', 'admin'));
