-- Retour arrière de 221_customer_accounts_v2 (avant tout usage en production :
-- les paiements en ligne reçus sont liés à des mouvements scellés).
DELETE FROM labels WHERE label_value IN ('ACCOUNT_IN_STRIPE', 'ACCOUNT_OUT_STRIPE') AND label_type = 'mop' AND lang = 'FR';
DROP TABLE IF EXISTS customer_account_online_payments;
DROP TABLE IF EXISTS customer_account_payment_links;
DROP TABLE IF EXISTS customer_account_reminders;
ALTER TABLE merchant_parameters
  DROP COLUMN IF EXISTS customer_account_reminder_sms,
  DROP COLUMN IF EXISTS customer_account_reminder_max,
  DROP COLUMN IF EXISTS customer_account_reminder_days,
  DROP COLUMN IF EXISTS customer_account_reminder_enabled,
  DROP COLUMN IF EXISTS customer_account_cash_mode;
DROP TABLE IF EXISTS customer_account_settings;
