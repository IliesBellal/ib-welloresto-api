-- Reverts 120_drop_cart_discount_legacy_columns.up.sql.
-- Columns are recreated empty: their historical values (already 100% NULL/
-- unused per Phase 1 recon) cannot be restored, and were never meaningful.

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS cart_discount_id varchar(64),
    ADD COLUMN IF NOT EXISTS cart_discount_code varchar(64);
