-- Reverts 171_drop_orders_fiscal_chain_index.up.sql (recrée l'index de la
-- migration 169, utile seulement au code du lot A).
--
-- Hors bloc transactionnel.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_fiscal_chain_head
    ON orders (merchant_id, delivered_on DESC, order_id DESC)
    WHERE hash IS NOT NULL;
