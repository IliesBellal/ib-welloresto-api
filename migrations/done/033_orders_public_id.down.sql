DROP TRIGGER trg_orders_public_id;

ALTER TABLE orders DROP INDEX idx_orders_public_id;
ALTER TABLE orders DROP COLUMN public_id;
