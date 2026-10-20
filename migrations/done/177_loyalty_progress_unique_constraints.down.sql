-- Reverts 177_loyalty_progress_unique_constraints.up.sql.
--
-- Hors bloc transactionnel (symétrique à CREATE INDEX CONCURRENTLY côté up).
DROP INDEX CONCURRENTLY IF EXISTS idx_customer_loyalty_progress_customer_program;
DROP INDEX CONCURRENTLY IF EXISTS idx_customer_loyalty_progress_order_order_program;
