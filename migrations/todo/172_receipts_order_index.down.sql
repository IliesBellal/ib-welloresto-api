-- Reverts 172_receipts_order_index.up.sql.
--
-- Hors bloc transactionnel.
DROP INDEX CONCURRENTLY IF EXISTS idx_receipts_order_id;
