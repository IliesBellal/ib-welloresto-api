-- Rollback de 152_cds_media_default_duration.up.sql.
--
-- ⚠ Rollback avec perte d'information : les médias qui SUIVAIENT la durée par
-- défaut reçoivent la valeur effective de leur écran, figée. La distinction
-- « suit le défaut » / « durée propre » n'existe plus dans l'ancien modèle.

-- 1. Figer la durée effective sur chaque média (les vidéos reprennent le
--    défaut de la colonne, l'ancien code exigeait une valeur).
UPDATE cds_media_items m
SET duration_seconds = COALESCE(
        m.duration_seconds,
        (SELECT s.default_media_duration_seconds FROM cds_settings s WHERE s.display_id = m.display_id),
        10
    )
WHERE m.duration_seconds IS NULL;

-- 2. Retour à l'ancienne forme de colonne.
ALTER TABLE cds_media_items
    ALTER COLUMN duration_seconds SET DEFAULT 10,
    ALTER COLUMN duration_seconds SET NOT NULL;

COMMENT ON COLUMN cds_media_items.duration_seconds IS NULL;

-- 3. Retrait du réglage global.
ALTER TABLE cds_settings
    DROP CONSTRAINT IF EXISTS cds_settings_default_media_duration_check;
ALTER TABLE cds_settings
    DROP COLUMN IF EXISTS default_media_duration_seconds;
