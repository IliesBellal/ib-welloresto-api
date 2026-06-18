ALTER TABLE subscriptions DROP COLUMN max_kiosks;

ALTER TABLE orders DROP INDEX idx_orders_kiosk;
ALTER TABLE orders DROP COLUMN kiosk_id;

ALTER TABLE products DROP COLUMN is_available_on_kiosk;
