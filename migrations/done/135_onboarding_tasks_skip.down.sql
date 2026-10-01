-- Reverts 135_onboarding_tasks_skip.up.sql.
ALTER TABLE onboarding_tasks
    DROP COLUMN IF EXISTS skip_reason,
    DROP COLUMN IF EXISTS skipped_at;
