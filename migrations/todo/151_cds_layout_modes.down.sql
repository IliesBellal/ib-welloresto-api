-- Rollback de 151_cds_layout_modes.up.sql.
--
-- ⚠ Rollback avec perte d'information : l'ancien modèle ne sait pas exprimer
-- « bandeau en bas ». marketing_bottom est ramené à three_zones avec le
-- marketing activé, c'est-à-dire à un bandeau à DROITE.

ALTER TABLE cds_settings
    ADD COLUMN IF NOT EXISTS marketing_enabled boolean NOT NULL DEFAULT false;

ALTER TABLE cds_settings
    DROP CONSTRAINT IF EXISTS cds_settings_layout_mode_check;

UPDATE cds_settings
SET marketing_enabled = (layout_mode IN ('marketing_right', 'marketing_bottom')),
    layout_mode = CASE
        WHEN layout_mode IN ('marketing_right', 'marketing_bottom') THEN 'three_zones'
        ELSE 'two_zones'
    END;

ALTER TABLE cds_settings
    ADD CONSTRAINT cds_settings_layout_mode_check
        CHECK (layout_mode IN ('two_zones', 'three_zones')),
    ALTER COLUMN layout_mode SET DEFAULT 'two_zones';
