-- Durée d'affichage des médias : un réglage GLOBAL par écran, et une
-- SURCHARGE individuelle facultative par média.
--
-- Avant : chaque média portait une durée figée à sa création (10 s par
-- défaut), sans moyen de la modifier ensuite ni de régler tous les médias
-- d'un coup. Changer la cadence d'une campagne de vingt visuels demandait de
-- tous les supprimer et les recréer.
--
-- Après :
--   cds_settings.default_media_duration_seconds   durée par défaut de l'écran ;
--   cds_media_items.duration_seconds              NULL = suit la durée par
--                                                 défaut, sinon durée propre
--                                                 à ce média.
-- La durée effective est COALESCE(duration_seconds, default_media_duration_seconds),
-- résolue côté serveur : l'écran reçoit toujours un entier et n'a rien à savoir
-- de cette distinction.
--
-- Une vidéo est jouée en entier : sa durée n'a pas de sens et reste NULL.

-- 1. Durée par défaut de l'écran. Mêmes bornes que la contrainte des médias.
ALTER TABLE cds_settings
    ADD COLUMN IF NOT EXISTS default_media_duration_seconds integer NOT NULL DEFAULT 10;

ALTER TABLE cds_settings
    DROP CONSTRAINT IF EXISTS cds_settings_default_media_duration_check;
ALTER TABLE cds_settings
    ADD CONSTRAINT cds_settings_default_media_duration_check
        CHECK (default_media_duration_seconds BETWEEN 3 AND 120);

-- 2. La durée d'un média devient facultative.
--    cds_media_items_duration_check (BETWEEN 3 AND 120) n'est pas touchée : un
--    CHECK est satisfait par NULL, seules les valeurs renseignées sont bornées.
ALTER TABLE cds_media_items
    ALTER COLUMN duration_seconds DROP NOT NULL,
    ALTER COLUMN duration_seconds DROP DEFAULT;

-- 3. Conversion de l'existant. 10 était le défaut de la colonne : une ligne à
--    10 n'a presque jamais été réglée volontairement, elle doit donc SUIVRE le
--    nouveau réglage global plutôt que rester figée à 10. Toute autre valeur a
--    été choisie explicitement et devient une surcharge individuelle.
--    Sans cela, changer la durée globale ne ferait rien sur les médias
--    existants, ce qui serait déroutant.
--
--    Les vidéos passent à NULL : leur durée n'a jamais été utilisée.
UPDATE cds_media_items
SET duration_seconds = NULL
WHERE duration_seconds = 10 OR kind = 'video';

COMMENT ON COLUMN cds_media_items.duration_seconds IS 'Durée d''affichage propre à ce média, en secondes. NULL = suit cds_settings.default_media_duration_seconds. Toujours NULL pour une vidéo (jouée en entier).';
COMMENT ON COLUMN cds_settings.default_media_duration_seconds IS 'Durée d''affichage par défaut des images et QR codes de la rotation marketing, en secondes.';
