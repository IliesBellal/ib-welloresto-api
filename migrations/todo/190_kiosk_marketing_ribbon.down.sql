-- Rollback de 190_kiosk_marketing_ribbon.up.sql.
-- À jouer APRÈS le retrait du code qui lit ou écrit kiosk_marketing_media et
-- les colonnes marketing_* de kiosk_settings (module kiosk : marketing*.go,
-- GetSettingsByMerchant, UpsertSettings). Les médias de la rotation sont perdus
-- (les fichiers R2 restent dans le bucket).

DROP TABLE IF EXISTS kiosk_marketing_media;

ALTER TABLE kiosk_settings
    DROP CONSTRAINT IF EXISTS kiosk_settings_marketing_default_duration_check;
ALTER TABLE kiosk_settings
    DROP COLUMN IF EXISTS marketing_default_duration_seconds,
    DROP COLUMN IF EXISTS marketing_ribbon_enabled;
