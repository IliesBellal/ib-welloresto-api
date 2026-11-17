-- Heure cuisine choisie à la main (docs/order-lifecycle.md, « Heure cuisine
-- manuelle »).
--
-- production_ready_at est normalement recalculée à chaque écriture de la
-- commande (estimated_ready - trajet, voir resolveProductionReadyAt). Le
-- personnel peut désormais l'avancer (regrouper des commandes à produire
-- ensemble) sans toucher à l'heure de livraison : ce drapeau dit que la
-- valeur en base est un choix manuel, à conserver tant qu'elle reste au plus
-- tard l'heure calculée.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code (le code lit la colonne).
-- Rejouable.
ALTER TABLE orders
  ADD COLUMN IF NOT EXISTS production_ready_manual boolean NOT NULL DEFAULT false;
