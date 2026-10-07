-- Reverts 169_fiscal_chain_indexes.up.sql.
--
-- Hors bloc transactionnel, une instruction à la fois (symétrique à CREATE
-- INDEX CONCURRENTLY côté up). Retirer l'index unique réautorise les numéros
-- de ticket en double : à ne faire qu'avec le retour au code d'avant le lot A.
DROP INDEX CONCURRENTLY IF EXISTS idx_cash_registers_fiscal_chain_head;
DROP INDEX CONCURRENTLY IF EXISTS idx_audit_logs_fiscal_chain_head;
DROP INDEX CONCURRENTLY IF EXISTS uq_receipts_merchant_receipt_number;
DROP INDEX CONCURRENTLY IF EXISTS idx_receipts_fiscal_chain_head;
DROP INDEX CONCURRENTLY IF EXISTS idx_orders_fiscal_chain_head;
DROP INDEX CONCURRENTLY IF EXISTS idx_payments_fiscal_chain_head;
