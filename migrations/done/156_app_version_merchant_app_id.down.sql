-- Rollback de 156_app_version_merchant_app_id.up.sql.
ALTER TABLE app_version_merchant DROP COLUMN IF EXISTS app_id;
