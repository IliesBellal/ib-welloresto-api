-- Reverts 130_users_google_sub.up.sql.
ALTER TABLE users DROP COLUMN IF EXISTS google_sub;
