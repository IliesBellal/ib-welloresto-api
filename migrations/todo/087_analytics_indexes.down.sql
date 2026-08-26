-- Retrait des index de support Analyses.
--
-- ATTENTION - NE PAS EXECUTER DANS UNE TRANSACTION : DROP INDEX CONCURRENTLY
-- est refuse a l'interieur d'un bloc transactionnel, comme sa contrepartie
-- CREATE. Jouer les instructions une par une.
--
-- IF EXISTS couvre le cas d'un CREATE INDEX CONCURRENTLY interrompu : l'index
-- laisse en etat "invalid" porte le meme nom et doit etre supprime ici avant de
-- rejouer le .up.sql.

DROP INDEX CONCURRENTLY IF EXISTS idx_payments_order_id;
DROP INDEX CONCURRENTLY IF EXISTS idx_extra_order_item_id;
DROP INDEX CONCURRENTLY IF EXISTS idx_orderitems_order_id;
DROP INDEX CONCURRENTLY IF EXISTS idx_orders_merchant_creation;
