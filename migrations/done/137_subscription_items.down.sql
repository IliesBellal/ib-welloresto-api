-- Reverts 137_subscription_items.up.sql.
ALTER TABLE subscriptions
    DROP COLUMN IF EXISTS override_price_cents,
    DROP COLUMN IF EXISTS billing_cycle,
    DROP COLUMN IF EXISTS current_period_end,
    DROP COLUMN IF EXISTS status;

DROP TABLE IF EXISTS subscription_items;
