-- Reverts 110_drop_dead_legacy_rights_columns.up.sql.
--
-- Restores the 5 columns with their original types/defaults (see
-- docs/migration-postgres/04-schema-postgres-target.sql). Data is NOT
-- restored (same irrecoverable-data caveat as every other DROP COLUMN in
-- this migration set, e.g. migration 100's and 104's down) — a restored
-- merchant gets the default value back, not whatever was there before.

DO $$
BEGIN
  IF to_regclass('public.users_rights') IS NOT NULL THEN
    ALTER TABLE users_rights
      ADD COLUMN IF NOT EXISTS access_wrdelivery boolean NOT NULL DEFAULT true,
      ADD COLUMN IF NOT EXISTS access_wrwaiter boolean NOT NULL DEFAULT true,
      ADD COLUMN IF NOT EXISTS export_reports boolean NOT NULL DEFAULT false,
      ADD COLUMN IF NOT EXISTS export_financials boolean NOT NULL DEFAULT false,
      ADD COLUMN IF NOT EXISTS export_customers boolean NOT NULL DEFAULT false;
  END IF;
END
$$;
