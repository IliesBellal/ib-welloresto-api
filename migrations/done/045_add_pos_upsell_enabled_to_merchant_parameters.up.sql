ALTER TABLE merchant_parameters
ADD COLUMN IF NOT EXISTS pos_upsell_enabled TINYINT(1) NOT NULL DEFAULT 0 AFTER pos_auto_lock_delay_minutes;
