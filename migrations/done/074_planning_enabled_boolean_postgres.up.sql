-- PostgreSQL migration: normalize planning publish-path flags to BOOLEAN.
-- Fixes errors like: operator does not exist: integer = boolean (SQLSTATE 42883)
-- when queries use "enabled = TRUE".

DO $$
BEGIN
  IF to_regclass('public.planning_weeks') IS NOT NULL THEN
    ALTER TABLE planning_weeks
      ALTER COLUMN enabled TYPE boolean
      USING (
        CASE
          WHEN enabled IS NULL THEN FALSE
          WHEN lower(enabled::text) IN ('1', 't', 'true', 'y', 'yes', 'on') THEN TRUE
          ELSE FALSE
        END
      ),
      ALTER COLUMN enabled SET DEFAULT TRUE,
      ALTER COLUMN enabled SET NOT NULL;
  END IF;

  IF to_regclass('public.planning_shifts') IS NOT NULL THEN
    ALTER TABLE planning_shifts
      ALTER COLUMN enabled TYPE boolean
      USING (
        CASE
          WHEN enabled IS NULL THEN FALSE
          WHEN lower(enabled::text) IN ('1', 't', 'true', 'y', 'yes', 'on') THEN TRUE
          ELSE FALSE
        END
      ),
      ALTER COLUMN enabled SET DEFAULT TRUE,
      ALTER COLUMN enabled SET NOT NULL;
  END IF;

  IF to_regclass('public.planning_positions') IS NOT NULL THEN
    ALTER TABLE planning_positions
      ALTER COLUMN enabled TYPE boolean
      USING (
        CASE
          WHEN enabled IS NULL THEN FALSE
          WHEN lower(enabled::text) IN ('1', 't', 'true', 'y', 'yes', 'on') THEN TRUE
          ELSE FALSE
        END
      ),
      ALTER COLUMN enabled SET DEFAULT TRUE,
      ALTER COLUMN enabled SET NOT NULL;
  END IF;

  IF to_regclass('public.employees') IS NOT NULL THEN
    ALTER TABLE employees
      ALTER COLUMN enabled TYPE boolean
      USING (
        CASE
          WHEN enabled IS NULL THEN FALSE
          WHEN lower(enabled::text) IN ('1', 't', 'true', 'y', 'yes', 'on') THEN TRUE
          ELSE FALSE
        END
      ),
      ALTER COLUMN enabled SET DEFAULT TRUE,
      ALTER COLUMN enabled SET NOT NULL;
  END IF;

  IF to_regclass('public.users') IS NOT NULL THEN
    ALTER TABLE users
      ALTER COLUMN enabled TYPE boolean
      USING (
        CASE
          WHEN enabled IS NULL THEN FALSE
          WHEN lower(enabled::text) IN ('1', 't', 'true', 'y', 'yes', 'on') THEN TRUE
          ELSE FALSE
        END
      ),
      ALTER COLUMN enabled SET DEFAULT TRUE,
      ALTER COLUMN enabled SET NOT NULL;
  END IF;

  IF to_regclass('public.planning_settings') IS NOT NULL THEN
    ALTER TABLE planning_settings
      ALTER COLUMN enabled TYPE boolean
      USING (
        CASE
          WHEN enabled IS NULL THEN FALSE
          WHEN lower(enabled::text) IN ('1', 't', 'true', 'y', 'yes', 'on') THEN TRUE
          ELSE FALSE
        END
      ),
      ALTER COLUMN enabled SET DEFAULT TRUE,
      ALTER COLUMN enabled SET NOT NULL;

    IF EXISTS (
      SELECT 1
      FROM information_schema.columns
      WHERE table_schema = 'public'
        AND table_name = 'planning_settings'
        AND column_name = 'planning_sms_notifications_enabled'
    ) THEN
      ALTER TABLE planning_settings
        ALTER COLUMN planning_sms_notifications_enabled TYPE boolean
        USING (
          CASE
            WHEN planning_sms_notifications_enabled IS NULL THEN FALSE
            WHEN lower(planning_sms_notifications_enabled::text) IN ('1', 't', 'true', 'y', 'yes', 'on') THEN TRUE
            ELSE FALSE
          END
        ),
        ALTER COLUMN planning_sms_notifications_enabled SET DEFAULT FALSE,
        ALTER COLUMN planning_sms_notifications_enabled SET NOT NULL;
    END IF;
  END IF;
END
$$;
