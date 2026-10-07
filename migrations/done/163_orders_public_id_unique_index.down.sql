-- Reverts 163_orders_public_id_unique_index.up.sql.
--
-- Hors bloc transactionnel (symétrique à CREATE INDEX CONCURRENTLY côté up).
DROP INDEX CONCURRENTLY IF EXISTS idx_orders_public_id;
