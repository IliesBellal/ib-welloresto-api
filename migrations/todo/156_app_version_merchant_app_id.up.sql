-- `app_version_merchant` restreint une version à une liste de merchants
-- (déploiement progressif), mais ne portait qu'un `version_code` — sans
-- `app_id`. `version_code` est un compteur choisi indépendamment par
-- chaque app (POS, kiosk...), donc deux apps peuvent parfaitement se
-- retrouver un jour avec le même entier : la restriction de rollout de
-- l'une aurait alors pu s'appliquer par erreur à l'autre
-- (auth.AuthRepository.CheckAppVersion / kiosk.Repository.CheckAppVersion
-- lisaient cette table par version_code seul). Voir
-- docs/KIOSK_DECISIONS.md, "Mise à jour automatique".
--
-- Aucune ligne kiosk n'existe encore dans cette table à ce jour (fonctionnalité
-- pas encore déployée) : tout le contenu actuel appartient donc au POS,
-- backfill sûr vers 'WR_RECEPTION' (voir wello_resto_flutter/lib/environment.dart,
-- Environment.appName, valeur envoyée telle quelle par le POS à
-- `POST /app/version/check`).
ALTER TABLE app_version_merchant ADD COLUMN IF NOT EXISTS app_id varchar(25);
UPDATE app_version_merchant SET app_id = 'WR_RECEPTION' WHERE app_id IS NULL;
ALTER TABLE app_version_merchant ALTER COLUMN app_id SET NOT NULL;
