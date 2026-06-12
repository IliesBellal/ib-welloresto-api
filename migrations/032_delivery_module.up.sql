-- Delivery module (driver app): per-stop FSM, current stop pointer, position
-- history, delivery instructions.
--
-- Status vocabulary (see docs/DELIVERY_DESIGN.md §0.4):
--   - delivery_session_order.status (this migration, brand new column): snake_case
--     textual values - pending / en_route / arrived / delivered / failed / canceled.
--   - orders.brand_status: existing UPPER convention, unchanged by this migration.
--     A new value 'DELIVERY_FAILED' will be written by the future per-stop "failed"
--     transition (Go work, not part of this migration).
--   - delivery_session.status: existing legacy values ('1'/'PENDING'/'0'/'DONE'/
--     'CANCELED'/'FINISHED') are NOT touched here. Normalizing them to
--     active/done/canceled is a separate future pass - see
--     docs/DELIVERY_DESIGN.md §7. Do not add numeric coercions on this column.
--
-- See docs/DELIVERY_DESIGN.md for the full FSM and the points flagged
-- [A VERIFIER] / [A VERIFIER EN BASE] before running this migration.

-- Per-stop status (FSM) and timestamps/reason for each delivery_session_order row.
ALTER TABLE delivery_session_order ADD COLUMN status VARCHAR(20) NOT NULL DEFAULT 'pending';
ALTER TABLE delivery_session_order ADD COLUMN arrived_at DATETIME NULL DEFAULT NULL;
ALTER TABLE delivery_session_order ADD COLUMN delivered_at DATETIME NULL DEFAULT NULL;
ALTER TABLE delivery_session_order ADD COLUMN failed_at DATETIME NULL DEFAULT NULL;
ALTER TABLE delivery_session_order ADD COLUMN canceled_at DATETIME NULL DEFAULT NULL;
ALTER TABLE delivery_session_order ADD COLUMN fail_reason VARCHAR(255) NULL DEFAULT NULL;

-- Current stop pointer for the session (NULL = none selected yet).
-- Type assumed to match orders.order_id (INT UNSIGNED AUTO_INCREMENT).
-- [A VERIFIER EN BASE] run `SHOW CREATE TABLE orders` / `SHOW CREATE TABLE delivery_session_order`
-- and adjust this type if order_id is not an INT UNSIGNED. No FK constraint added,
-- consistent with the existing delivery_session_order.order_id column (no FK to
-- historical tables anywhere in this codebase).
ALTER TABLE delivery_session ADD COLUMN current_order_id INT UNSIGNED NULL DEFAULT NULL;
CREATE INDEX idx_delivery_session_current_order ON delivery_session (current_order_id);

-- Driver position freshness.
-- NOTE: `users.heading` already exists (selected as `u.heading` by the
-- user_status_view definition) - NOT added here, see docs/DELIVERY_DESIGN.md §0.1.
ALTER TABLE users ADD COLUMN last_position_at DATETIME NULL DEFAULT NULL;

-- Position history, written only while a delivery session is active.
-- delivery_session_id type assumed to match delivery_session.id (INT UNSIGNED AUTO_INCREMENT).
-- [A VERIFIER EN BASE] adjust if delivery_session.id is not INT UNSIGNED.
CREATE TABLE delivery_position (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id VARCHAR(64) NOT NULL,
    delivery_session_id INT UNSIGNED NOT NULL,
    lat DECIMAL(10,7) NOT NULL,
    lng DECIMAL(10,7) NOT NULL,
    heading FLOAT NULL DEFAULT NULL,
    accuracy FLOAT NULL DEFAULT NULL,
    speed FLOAT NULL DEFAULT NULL,
    recorded_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_delivery_position_session (delivery_session_id, recorded_at),
    KEY idx_delivery_position_user (user_id, recorded_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Durable, customer-level delivery instructions (digicode, floor, etc.).
ALTER TABLE customer ADD COLUMN delivery_notes TEXT NULL DEFAULT NULL;
