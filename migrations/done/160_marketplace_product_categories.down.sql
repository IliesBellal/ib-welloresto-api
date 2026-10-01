-- Reverts 160_marketplace_product_categories.up.sql. À n'appliquer qu'après
-- avoir redéployé le code qui écrit UBER_EATS_TEMP / aucune catégorie
-- (CreateExternalProductTx, deliveroo_orders.SyncProduct). Les produits
-- Deliveroo retrouvent '' — leur valeur d'avant migration.
UPDATE products
SET category = 'UBER_EATS_TEMP'
WHERE category = 'UBER_EATS';

UPDATE products
SET category = ''
WHERE category = 'DELIVEROO';
