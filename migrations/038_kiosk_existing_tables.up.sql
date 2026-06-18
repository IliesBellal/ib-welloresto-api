-- Kiosk module (increment 1): columns added to existing tables.
-- is_available_on_kiosk defaults to TRUE so every existing product stays
-- visible once the module is activated for a merchant (no manual backfill
-- needed) — see docs/KIOSK_DECISIONS.md section G.2.
-- orders.kiosk_id stores kiosks.public_id (VARCHAR, no FK to a historical
-- table, consistent with the rest of this codebase).

ALTER TABLE products
    ADD COLUMN is_available_on_kiosk BOOLEAN NOT NULL DEFAULT TRUE AFTER is_available_on_sno;

ALTER TABLE orders
    ADD COLUMN kiosk_id VARCHAR(64) NULL DEFAULT NULL AFTER cash_register_id;
ALTER TABLE orders
    ADD INDEX idx_orders_kiosk (kiosk_id);

ALTER TABLE subscriptions
    ADD COLUMN max_kiosks INT NOT NULL DEFAULT 0 COMMENT 'Nombre max de bornes actives (0 = module non inclus)';
