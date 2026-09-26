-- Site vitrine : origine de la visite (paramètres utm_* de l'URL) enregistrée
-- avec chaque demande de démo — première campagne concernée : le dépliant
-- papier, dont le QR code renvoie vers /rdv?utm_source=depliant&
-- utm_medium=print&utm_campaign=dpl_sno_brn_2610. Voir
-- wello-resto-vitrine/docs/decisions-log.md (2026-09-26).
--
-- Colonnes facultatives : la plupart des demandes arrivent sans utm_* (visite
-- directe du site), NULL dans ce cas — jamais une chaîne vide.
--
-- ORDRE DE DÉPLOIEMENT : appliquer cette migration AVANT de déployer le code
-- qui les écrit (Repository.CreateBooking insère ces trois colonnes) — dans
-- l'ordre inverse, chaque demande de démo échouerait en erreur SQL.
ALTER TABLE demo_bookings
    ADD COLUMN IF NOT EXISTS utm_source   text,
    ADD COLUMN IF NOT EXISTS utm_medium   text,
    ADD COLUMN IF NOT EXISTS utm_campaign text;

COMMENT ON COLUMN demo_bookings.utm_source IS 'Paramètre utm_source de la page du formulaire (ex. depliant). NULL si absent.';
COMMENT ON COLUMN demo_bookings.utm_medium IS 'Paramètre utm_medium de la page du formulaire (ex. print, email). NULL si absent.';
COMMENT ON COLUMN demo_bookings.utm_campaign IS 'Paramètre utm_campaign de la page du formulaire (ex. dpl_sno_brn_2610). NULL si absent.';
