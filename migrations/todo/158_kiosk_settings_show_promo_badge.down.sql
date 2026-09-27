-- Reverts 158_kiosk_settings_show_promo_badge.up.sql. À n'appliquer qu'après
-- avoir retiré du code déployé la lecture/écriture de show_promo_badge
-- (Repository.GetSettingsByMerchant/UpsertSettings).
ALTER TABLE kiosk_settings
    DROP COLUMN IF EXISTS show_promo_badge;
