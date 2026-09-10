-- Reverts 104_drop_role_job_title_shift_title_location.up.sql.
--
-- Restores the 7 columns as plain (non-enum) types — the up migration may
-- have also dropped `employees_role_enum`, and recreating a custom type
-- here isn't worth the complexity for an emergency rollback. Data is NOT
-- restored (same irrecoverable-data caveat as every other DROP COLUMN in
-- this migration set, e.g. migration 100's down) — a restored merchant
-- gets empty columns, not the original values.

DO $$
BEGIN
  IF to_regclass('public.employees') IS NOT NULL THEN
    ALTER TABLE employees
      ADD COLUMN IF NOT EXISTS role varchar(32) NOT NULL DEFAULT 'employee',
      ADD COLUMN IF NOT EXISTS job_title varchar(150) NULL;
  END IF;

  IF to_regclass('public.users_rights') IS NOT NULL THEN
    ALTER TABLE users_rights
      ADD COLUMN IF NOT EXISTS role varchar(32) NOT NULL DEFAULT 'employee',
      ADD COLUMN IF NOT EXISTS job_title varchar(150) NULL;
  END IF;

  IF to_regclass('public.planning_shifts') IS NOT NULL THEN
    ALTER TABLE planning_shifts
      ADD COLUMN IF NOT EXISTS title varchar(150) NOT NULL DEFAULT '',
      ADD COLUMN IF NOT EXISTS location varchar(150) NULL;
  END IF;

  IF to_regclass('public.planning_published_shift_snapshots') IS NOT NULL THEN
    ALTER TABLE planning_published_shift_snapshots
      ADD COLUMN IF NOT EXISTS title varchar(255) NOT NULL DEFAULT '';
  END IF;
END
$$;
