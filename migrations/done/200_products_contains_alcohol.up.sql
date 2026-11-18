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
-- Lue par la synchro des menus marketplace et par la fiche produit du
-- back-office (GET /menu/products/{id}, case « Alcool » de l'onglet Général),
-- écrite seulement quand la fiche l'envoie. Le chargement commun des menus
-- POS / ScanNOrder / borne ne la lit pas.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code — sans elle, la fiche produit
-- du back-office et la publication Deliveroo échouent. Rejouable.

ALTER TABLE products
    ADD COLUMN IF NOT EXISTS contains_alcohol boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN products.contains_alcohol IS
    'Article soumis à contrôle d''âge (alcool...). Saisi par le restaurateur, publié vers Deliveroo / Uber Eats. Ne détermine pas la TVA.';
