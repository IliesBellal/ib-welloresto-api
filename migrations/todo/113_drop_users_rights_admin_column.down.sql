-- Reverts 113_drop_users_rights_admin_column.up.sql.
--
-- Restores the column with its original type/default (see
-- docs/migration-postgres/04-schema-postgres-target.sql). Data is NOT
-- restored (same irrecoverable-data caveat as every other DROP COLUMN in
-- this migration set, e.g. migrations 100/104/110's down) — a restored row
-- gets the default value back (false), not whatever admin flag it held
-- before the column was dropped.

DO $$
BEGIN
  IF to_regclass('public.users_rights') IS NOT NULL THEN
    ALTER TABLE users_rights
      ADD COLUMN IF NOT EXISTS admin boolean NOT NULL DEFAULT false;
  END IF;
END
$$;
