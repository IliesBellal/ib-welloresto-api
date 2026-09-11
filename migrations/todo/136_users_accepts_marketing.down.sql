-- Reverts 136_users_accepts_marketing.up.sql.
ALTER TABLE users DROP COLUMN IF EXISTS accepts_marketing;
