-- Rollback de 155_app_version_checksum.up.sql.
ALTER TABLE app_version DROP COLUMN IF EXISTS checksum_sha256;
