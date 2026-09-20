-- Disposition de l'écran : trois configurations au lieu de « deux/trois zones »
-- combiné à un interrupteur marketing.
--
--   marketing_right   bandeau marketing vertical, à droite
--   marketing_bottom  bandeau marketing horizontal, en bas
--   no_marketing      sans bandeau marketing
--
-- L'ancien modèle avait DEUX champs pour dire une seule chose
-- (layout_mode = 'three_zones' ET marketing_enabled = true), avec des états
-- incohérents possibles : trois zones sans marketing, ou marketing activé sur
-- une disposition à deux zones — qui n'affichait alors rien. Un seul champ
-- supprime la classe de bug ; marketing_enabled disparaît.
--
-- Les médias (cds_media_items) sont conservés quelle que soit la disposition :
-- passer à no_marketing puis revenir ne fait rien perdre au restaurateur.

-- 1. La contrainte porte sur l'ancien jeu de valeurs : elle doit sauter avant
--    la conversion des lignes, sinon l'UPDATE la violerait.
ALTER TABLE cds_settings
    DROP CONSTRAINT IF EXISTS cds_settings_layout_mode_check;

-- 2. Conversion. L'ancien code n'affichait le marketing QUE si les deux
--    conditions étaient réunies : c'est ce comportement qu'on préserve, pas la
--    valeur brute d'une colonne. Le bandeau était vertical à droite.
UPDATE cds_settings
SET layout_mode = CASE
        WHEN layout_mode = 'three_zones' AND marketing_enabled THEN 'marketing_right'
        ELSE 'no_marketing'
    END;

-- 3. Nouvelle contrainte et nouveau défaut.
ALTER TABLE cds_settings
    ADD CONSTRAINT cds_settings_layout_mode_check
        CHECK (layout_mode IN ('marketing_right', 'marketing_bottom', 'no_marketing')),
    ALTER COLUMN layout_mode SET DEFAULT 'no_marketing';

-- 4. Le second champ n'a plus de raison d'être.
ALTER TABLE cds_settings
    DROP COLUMN IF EXISTS marketing_enabled;
