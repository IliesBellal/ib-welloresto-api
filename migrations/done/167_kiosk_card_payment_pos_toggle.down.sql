-- Reverts 167_kiosk_card_payment_pos_toggle.up.sql. À n'appliquer qu'après
-- avoir retiré du code déployé la lecture/écriture de card_payment_pos_toggle
-- et card_payment_closed_at (Repository.GetSettingsByMerchant,
-- Repository.SetCardPaymentClosedAt).
ALTER TABLE kiosk_settings
    DROP COLUMN IF EXISTS card_payment_closed_at,
    DROP COLUMN IF EXISTS card_payment_pos_toggle;
