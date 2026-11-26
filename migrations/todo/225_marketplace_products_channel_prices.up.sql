-- Produits créés à la volée par les commandes Uber Eats / Deliveroo : prix
-- emporter et livraison manquants.
--
-- Ces produits n'avaient que `price` ; price_take_away et price_delivery
-- restaient au défaut 0. La caisse affiche (et imprime) une ligne Uber Eats ou
-- Deliveroo au prix livraison du produit, donc 0 €. Le code crée désormais les
-- trois prix (menu.CreateExternalProductTx, deliveroo_orders) ; cette migration
-- répare les produits existants en recopiant `price`.
--
-- Ne touche qu'aux colonnes encore à 0 (un prix saisi depuis le back-office
-- est conservé). Catégories : UBER_EATS, DELIVEROO, et UBER_EATS_TEMP (ancien
-- code, produits historiques).
--
-- ORDRE DE DÉPLOIEMENT : indifférent (aucun changement de schéma). Rejouable.
UPDATE products
SET price_take_away = price
WHERE category IN ('UBER_EATS', 'UBER_EATS_TEMP', 'DELIVEROO')
  AND price_take_away = 0
  AND price <> 0;

UPDATE products
SET price_delivery = price
WHERE category IN ('UBER_EATS', 'UBER_EATS_TEMP', 'DELIVEROO')
  AND price_delivery = 0
  AND price <> 0;
