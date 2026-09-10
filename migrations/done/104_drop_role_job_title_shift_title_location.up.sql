-- Cleanup: drops 7 columns identified as dead/never-consumed during a
-- back-office review session:
--
-- - `employees.role` / `users_rights.role` : a free-text "employee/manager/
--   admin" enum, distinct from the real RBAC role (`role_id`, RBAC lot 9).
--   Round-tripped through PATCH /users/{id}/member and /employees but never
--   read by any authorization check, POS/kiosk/Flutter screen, report or
--   export (grepped across ib-welloresto-api, wello-back-office,
--   wello_resto_flutter, wello-kiosk). `users_rights.role` was already
--   orphaned before this migration — nothing in current Go code reads or
--   writes it (the live "member" HR block lives on `employees` since the
--   employee/user decoupling work, see docs/audit-employee-user-decoupling.md).
-- - `employees.job_title` / `users_rights.job_title` ("Poste affiché") :
--   edited from 4 back-office UI surfaces, but no current Go request/
--   response DTO includes a JobTitle field — every save was already a
--   silent no-op against the real API (only the front-end mock faked it
--   working). Never displayed anywhere downstream either.
-- - `planning_shifts.title` ("Titre") : was the shift card's primary label
--   (`shift.title || shift.position || "Shift"`) — the back-office now
--   shows the position instead. Also dropped from the publish-notification
--   diff key and from `planning_published_shift_snapshots.title` (the
--   historical snapshot table used to detect what changed between two
--   publishes of the same week).
-- - `planning_shifts.location` ("Lieu") : free text, never rendered
--   anywhere (grepped the back-office, Flutter apps, and every report).
--
-- Postgres syntax + defensive to_regclass/IF EXISTS guards, matching
-- migration 074 (this schema went through a MySQL -> Postgres conversion
-- and these guards let the file replay safely regardless of environment
-- state). `DROP COLUMN IF EXISTS` is idempotent on its own in Postgres;
-- to_regclass additionally guards against the table not existing yet.

DO $$
BEGIN
  IF to_regclass('public.employees') IS NOT NULL THEN
    ALTER TABLE employees
      DROP COLUMN IF EXISTS role,
      DROP COLUMN IF EXISTS job_title;
  END IF;

  IF to_regclass('public.users_rights') IS NOT NULL THEN
    ALTER TABLE users_rights
      DROP COLUMN IF EXISTS role,
      DROP COLUMN IF EXISTS job_title;
  END IF;

  IF to_regclass('public.planning_shifts') IS NOT NULL THEN
    ALTER TABLE planning_shifts
      DROP COLUMN IF EXISTS title,
      DROP COLUMN IF EXISTS location;
  END IF;

  IF to_regclass('public.planning_published_shift_snapshots') IS NOT NULL THEN
    ALTER TABLE planning_published_shift_snapshots
      DROP COLUMN IF EXISTS title;
  END IF;
END
$$;

-- `employees.role` may have been converted to a dedicated enum type
-- (e.g. `employees_role_enum`) during the MySQL -> Postgres conversion.
-- Drop it too, if present and now unused — DROP TYPE fails loudly (not
-- silently) if anything else still references it, so this is safe to
-- attempt unconditionally.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'employees_role_enum') THEN
    DROP TYPE employees_role_enum;
  END IF;
END
$$;
