-- Ruban marketing des bornes Kiosk : rotation d'images et de vidéos affichée
-- en haut de l'écran de menu (wello-kiosk).
--
-- Périmètre MERCHANT, comme kiosk_settings : toutes les bornes d'un même
-- établissement affichent la même rotation. (Le CDS, lui, est par écran :
-- cds_settings / cds_media_items — migrations 147, 151, 152.)
--
--   kiosk_settings.marketing_ribbon_enabled              interrupteur global,
--                                                        désactivé par défaut :
--                                                        aucune borne ne change
--                                                        de comportement tant
--                                                        que le restaurateur ne
--                                                        l'active pas.
--   kiosk_settings.marketing_default_duration_seconds    durée par défaut d'une
--                                                        image.
--   kiosk_marketing_media                                la rotation.
--
-- Durée d'un média : NULL = suit la durée par défaut du merchant, sinon durée
-- propre. Résolue côté serveur (COALESCE) : la borne reçoit toujours un entier.
-- Une vidéo est jouée en entier, muette : sa durée reste NULL.
--
-- Pas de type 'qr' (décision produit) : un QR pourra s'ajouter plus tard en
-- élargissant la contrainte kiosk_marketing_media_kind_check.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. GetSettingsByMerchant
-- sélectionne les deux nouvelles colonnes ; sans elles, GET /kiosk/settings
-- échoue et les bornes ne peuvent plus charger leurs réglages.

-- 1. Réglages du ruban.
ALTER TABLE kiosk_settings
    ADD COLUMN IF NOT EXISTS marketing_ribbon_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS marketing_default_duration_seconds integer NOT NULL DEFAULT 10;

ALTER TABLE kiosk_settings
    DROP CONSTRAINT IF EXISTS kiosk_settings_marketing_default_duration_check;
ALTER TABLE kiosk_settings
    ADD CONSTRAINT kiosk_settings_marketing_default_duration_check
        CHECK (marketing_default_duration_seconds BETWEEN 3 AND 120);

COMMENT ON COLUMN kiosk_settings.marketing_ribbon_enabled IS 'Affiche le ruban marketing en haut de l''écran de menu des bornes.';
COMMENT ON COLUMN kiosk_settings.marketing_default_duration_seconds IS 'Durée d''affichage par défaut des images du ruban marketing, en secondes (3 à 120).';

-- 2. Rotation. id varchar(64) généré côté Go (helpers.GeneratePrefixedID),
--    aucune FK vers les tables historiques (convention des migrations récentes).
CREATE TABLE IF NOT EXISTS kiosk_marketing_media (
    id               varchar(64) PRIMARY KEY,
    merchant_id      varchar(64) NOT NULL,
    kind             varchar(10) NOT NULL,
    url              text NOT NULL,
    duration_seconds integer,
    sort_order       integer NOT NULL DEFAULT 0,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz,
    CONSTRAINT kiosk_marketing_media_kind_check
        CHECK (kind IN ('image', 'video')),
    -- NULL satisfait un CHECK : seules les durées renseignées sont bornées.
    CONSTRAINT kiosk_marketing_media_duration_check
        CHECK (duration_seconds BETWEEN 3 AND 120)
);

CREATE INDEX IF NOT EXISTS idx_kiosk_marketing_media_merchant
    ON kiosk_marketing_media (merchant_id, sort_order);

COMMENT ON TABLE kiosk_marketing_media IS 'Rotation du ruban marketing des bornes Kiosk, par merchant. Voir docs/KIOSK_DECISIONS.md.';
COMMENT ON COLUMN kiosk_marketing_media.duration_seconds IS 'Durée propre à ce média, en secondes. NULL = suit kiosk_settings.marketing_default_duration_seconds. Toujours NULL pour une vidéo (jouée en entier).';
