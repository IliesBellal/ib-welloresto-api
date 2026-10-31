-- Numéro de build (versionCode Android, suffixe « +N » du pubspec) remonté par
-- chaque borne à son heartbeat, en complément de kiosks.app_version (version
-- affichable, « 1.0.0 »). C'est ce numéro que compare POST /kiosk/app/version-check,
-- et lui seul permet de savoir quelles bornes n'ont pas encore reçu une mise à jour.
--
-- Colonne nullable, sans défaut : les bornes dont l'appli est antérieure à cette
-- fonctionnalité ne l'envoient pas, NULL = « jamais remontée ».
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Rejouable.

ALTER TABLE kiosks
    ADD COLUMN IF NOT EXISTS version_code integer NULL;
