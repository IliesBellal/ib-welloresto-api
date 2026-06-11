ALTER TABLE merchant_parameters
ADD COLUMN IF NOT EXISTS pos_auto_lock_enabled TINYINT(1) NOT NULL DEFAULT 0 AFTER pager_number_required;

ALTER TABLE merchant_parameters
ADD COLUMN IF NOT EXISTS pos_auto_lock_delay_minutes INT NOT NULL DEFAULT 5 AFTER pos_auto_lock_enabled;
