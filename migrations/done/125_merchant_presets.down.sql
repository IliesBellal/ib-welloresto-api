-- Reverts 125_merchant_presets.up.sql.
--
-- Ne restaure pas l'état d'activation_state d'avant migration (les marchands
-- LIVE issus du rattrapage n'ont pas de "vraie" valeur antérieure à
-- restaurer — même caveat que les autres DROP COLUMN de ce dépôt).

ALTER TABLE merchant
    DROP COLUMN IF EXISTS preset_code,
    DROP COLUMN IF EXISTS preset_version,
    DROP COLUMN IF EXISTS place_id,
    DROP COLUMN IF EXISTS activation_state,
    DROP COLUMN IF EXISTS went_live_at,
    DROP COLUMN IF EXISTS signup_channel,
    DROP COLUMN IF EXISTS signup_source;

DROP TABLE IF EXISTS merchant_presets;
