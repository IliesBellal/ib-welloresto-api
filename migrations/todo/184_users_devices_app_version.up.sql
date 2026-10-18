-- Version applicative remontée par chaque appareil enregistré (tablettes POS...).
-- Voir docs/DEVICE_APP_VERSIONS.md.
--
--   app_version      version affichable (pubspec « 1.7.6 »), 20 caractères max comme
--                    kiosks.app_version / cds_displays.app_version.
--   version_code     numéro de build (pubspec « +135 »), celui que compare
--                    POST /app/version/check.
--   app_version_at   dernière remontée de la version par l'appareil. NULL = jamais
--                    remontée (appli antérieure à cette fonctionnalité).
--
-- Colonnes nullables, sans défaut : la table peut être volumineuse et les
-- anciennes versions de l'appli ne les renseignent pas.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. L'écriture de la version est
-- isolée de l'enregistrement du token FCM (échec journalisé, jamais bloquant),
-- mais GET /device/versions lit ces colonnes.

ALTER TABLE users_devices
    ADD COLUMN IF NOT EXISTS app_version    varchar(20) NULL,
    ADD COLUMN IF NOT EXISTS version_code   integer     NULL,
    ADD COLUMN IF NOT EXISTS app_version_at timestamptz NULL;
