-- Rollback for 074_planning_enabled_boolean_postgres.up.sql
-- Restores integer storage (0/1) for the same columns.

DO $$
BEGIN
  IF to_regclass('public.planning_weeks') IS NOT NULL THEN
    ALTER TABLE planning_weeks
      ALTER COLUMN enabled TYPE integer
      USING (CASE WHEN enabled THEN 1 ELSE 0 END),
      ALTER COLUMN enabled SET DEFAULT 1,
      ALTER COLUMN enabled SET NOT NULL;
  END IF;

  IF to_regclass('public.planning_shifts') IS NOT NULL THEN
    ALTER TABLE planning_shifts
      ALTER COLUMN enabled TYPE integer
      USING (CASE WHEN enabled THEN 1 ELSE 0 END),
      ALTER COLUMN enabled SET DEFAULT 1,
      ALTER COLUMN enabled SET NOT NULL;
  END IF;

  IF to_regclass('public.planning_positions') IS NOT NULL THEN
    ALTER TABLE planning_positions
      ALTER COLUMN enabled TYPE integer
      USING (CASE WHEN enabled THEN 1 ELSE 0 END),
      ALTER COLUMN enabled SET DEFAULT 1,
      ALTER COLUMN enabled SET NOT NULL;
  END IF;

  IF to_regclass('public.employees') IS NOT NULL THEN
    ALTER TABLE employees
      ALTER COLUMN enabled TYPE integer
      USING (CASE WHEN enabled THEN 1 ELSE 0 END),
      ALTER COLUMN enabled SET DEFAULT 1,
      ALTER COLUMN enabled SET NOT NULL;
  END IF;

  IF to_regclass('public.users') IS NOT NULL THEN
    ALTER TABLE users
      ALTER COLUMN enabled TYPE integer
      USING (CASE WHEN enabled THEN 1 ELSE 0 END),
      ALTER COLUMN enabled SET DEFAULT 1,
      ALTER COLUMN enabled SET NOT NULL;
  END IF;

  IF to_regclass('public.planning_settings') IS NOT NULL THEN
    ALTER TABLE planning_settings
      ALTER COLUMN enabled TYPE integer
      USING (CASE WHEN enabled THEN 1 ELSE 0 END),
      ALTER COLUMN enabled SET DEFAULT 1,
      ALTER COLUMN enabled SET NOT NULL;

    IF EXISTS (
      SELECT 1
      FROM information_schema.columns
      WHERE table_schema = 'public'
        AND table_name = 'planning_settings'
        AND column_name = 'planning_sms_notifications_enabled'
    ) THEN
      ALTER TABLE planning_settings
        ALTER COLUMN planning_sms_notifications_enabled TYPE integer
        USING (CASE WHEN planning_sms_notifications_enabled THEN 1 ELSE 0 END),
        ALTER COLUMN planning_sms_notifications_enabled SET DEFAULT 0,
        ALTER COLUMN planning_sms_notifications_enabled SET NOT NULL;
    END IF;
  END IF;
END
$$;
