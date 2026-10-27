-- Bandeau d'annonce du hub de franchise (« Nouvelle ouverture à Nancy »,
-- « Burger du mois ») : réglé dans Franchise → Hub au back-office, affiché en
-- haut du hub ScanNOrder. Voir docs/franchises.md (D34).
--
-- Affiché quand announcement_enabled, avec un texte, et jusqu'au
-- announcement_ends_on inclus (jour local de la franchise) s'il est fixé.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Rejouable.

ALTER TABLE franchises ADD COLUMN IF NOT EXISTS announcement_enabled boolean NOT NULL DEFAULT false;
ALTER TABLE franchises ADD COLUMN IF NOT EXISTS announcement_text varchar(140);
-- Lien facultatif (https) ouvert au clic sur le bandeau.
ALTER TABLE franchises ADD COLUMN IF NOT EXISTS announcement_url varchar(500);
-- Dernier jour d'affichage, inclus ; NULL = sans fin.
ALTER TABLE franchises ADD COLUMN IF NOT EXISTS announcement_ends_on date;
