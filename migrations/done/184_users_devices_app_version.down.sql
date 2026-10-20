ALTER TABLE users_devices
    DROP COLUMN IF EXISTS app_version,
    DROP COLUMN IF EXISTS version_code,
    DROP COLUMN IF EXISTS app_version_at;
