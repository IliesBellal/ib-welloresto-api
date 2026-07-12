ALTER TABLE merchant_parameters
ADD COLUMN IF NOT EXISTS customer_form_requirements JSON NULL DEFAULT NULL AFTER pos_upsell_enabled;
