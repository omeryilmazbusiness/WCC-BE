-- +goose Up
-- Seed branch + demo users for local MVP (password: ChangeMe123!)
-- bcrypt hash generated for ChangeMe123!

INSERT INTO branches (id, code, name_en, name_ar)
VALUES
    ('11111111-1111-1111-1111-111111111111', 'HQ', 'Head Office', 'المكتب الرئيسي');

INSERT INTO users (id, email, password_hash, full_name, role, branch_id, is_active)
VALUES
    ('22222222-2222-2222-2222-222222222201', 'gm@wodi.local',
     '$2a$10$5CR8wFjAGXO1HO3elXTwvuEi1CGjWD74PK6foes35Zf2emMDpOzhK',
     'General Manager', 'gm', '11111111-1111-1111-1111-111111111111', TRUE),
    ('22222222-2222-2222-2222-222222222202', 'manager@wodi.local',
     '$2a$10$5CR8wFjAGXO1HO3elXTwvuEi1CGjWD74PK6foes35Zf2emMDpOzhK',
     'Branch Manager', 'manager', '11111111-1111-1111-1111-111111111111', TRUE),
    ('22222222-2222-2222-2222-222222222203', 'sales@wodi.local',
     '$2a$10$5CR8wFjAGXO1HO3elXTwvuEi1CGjWD74PK6foes35Zf2emMDpOzhK',
     'Sales Employee', 'employee', '11111111-1111-1111-1111-111111111111', TRUE);

-- +goose Down
DELETE FROM users WHERE email IN ('gm@wodi.local', 'manager@wodi.local', 'sales@wodi.local');
DELETE FROM branches WHERE code = 'HQ';
