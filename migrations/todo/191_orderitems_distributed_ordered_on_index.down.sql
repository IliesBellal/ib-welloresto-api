-- Reverts 191_orderitems_distributed_ordered_on_index.up.sql.
--
-- Hors bloc transactionnel.
DROP INDEX CONCURRENTLY IF EXISTS idx_orderitems_distributed_ordered_on;
