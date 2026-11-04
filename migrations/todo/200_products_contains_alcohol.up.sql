-- Produit contenant de l'alcool (ou tout article soumis à un contrôle d'âge).
--
-- Renseigné par le restaurateur lui-même, produit par produit, sans détection
-- automatique. Publié tel quel vers les plateformes de livraison
-- (contains_alcohol de chaque article Deliveroo ; Uber Eats ensuite). Ne pilote
-- PAS la TVA : le taux publié est le vrai taux livraison du produit
-- (tva_delivery_id).
--
-- Défaut false : aucun produit existant n'est marqué.
--
-- Seule la synchro des menus marketplace lit cette colonne, par une requête à
-- part (le chargement commun des menus POS / ScanNOrder / borne ne la lit pas).
-- Sans cette migration, la publication d'un menu Deliveroo échoue avec une
-- erreur explicite ; rien d'autre n'est touché.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Rejouable.

ALTER TABLE products
    ADD COLUMN IF NOT EXISTS contains_alcohol boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN products.contains_alcohol IS
    'Article soumis à contrôle d''âge (alcool...). Saisi par le restaurateur, publié vers Deliveroo / Uber Eats. Ne détermine pas la TVA.';
