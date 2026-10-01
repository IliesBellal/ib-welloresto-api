-- Reverts 162_order_item_remakes.up.sql. À n'appliquer qu'après avoir retiré
-- le code qui lit/écrit orderitems.remake_quantity (chargement des commandes,
-- SetDistributedProducts, retour en production) : l'historique des retours en
-- production est perdu.
DROP TABLE IF EXISTS order_item_remakes;
ALTER TABLE orderitems DROP COLUMN IF EXISTS remake_quantity;
