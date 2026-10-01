-- Reverts 128_users_auth_provider.up.sql.
ALTER TABLE users DROP COLUMN IF EXISTS auth_provider;
