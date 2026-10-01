-- Reverts 144_subscription_overrides_trial.up.sql.
ALTER TABLE subscription_overrides
    DROP COLUMN IF EXISTS trial_ends_at,
    DROP COLUMN IF EXISTS trial_reminder_7d_sent_at,
    DROP COLUMN IF EXISTS trial_reminder_1d_sent_at;
