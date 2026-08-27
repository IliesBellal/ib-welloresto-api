-- Drapeau d'acces au module Livraison (onglet "Livraison" du POS), au meme
-- titre que haccp_enabled/bookings_enabled/kiosks_enabled : override par
-- etablissement (subscriptions) avec fallback sur le plan (packages).
--
-- Defaut true pour ne rien changer aux etablissements existants : l'onglet
-- Livraison etait jusqu'ici accessible sans condition.

ALTER TABLE packages
    ADD COLUMN delivery_enabled boolean NOT NULL DEFAULT true;

ALTER TABLE subscriptions
    ADD COLUMN delivery_enabled boolean NOT NULL DEFAULT true;
