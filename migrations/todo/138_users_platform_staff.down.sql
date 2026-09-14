-- Reverts 138_users_platform_staff.up.sql.
ALTER TABLE users
    DROP COLUMN IF EXISTS is_platform_staff;
