ALTER TABLE packages
    DROP COLUMN delivery_enabled;

ALTER TABLE subscriptions
    DROP COLUMN delivery_enabled;
