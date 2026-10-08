-- Reverts 175_fiscal_period_indexes.up.sql (hors bloc transactionnel).
DROP INDEX CONCURRENTLY IF EXISTS idx_payments_merchant_date;
DROP INDEX CONCURRENTLY IF EXISTS idx_orders_merchant_closed;
