-- Codes promo panier (discount_scope ORDER_TOTAL) + audit des utilisations de codes.
-- Pas de FOREIGN KEY vers `discounts`/`orders`/`orders.order_id` : ces tables sont
-- legacy (créées avant cet outil de migration), leurs types exacts de colonnes ne
-- sont pas garantis ici. On sécurise via une UNIQUE KEY + des index simples.

ALTER TABLE discounts
    ADD COLUMN discount_scope ENUM('PRODUCT', 'ORDER_TOTAL') NOT NULL DEFAULT 'PRODUCT' AFTER discount_code,
    ADD COLUMN max_redemptions INT NULL DEFAULT NULL,
    ADD COLUMN max_redemptions_per_customer INT NULL DEFAULT NULL;

CREATE TABLE discount_redemptions (
    id                   BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    discount_id          VARCHAR(64) NOT NULL,
    order_id             BIGINT UNSIGNED NOT NULL,
    merchant_id          VARCHAR(64) NOT NULL,
    customer_id          VARCHAR(64) NULL,
    amount_applied_cents INT NOT NULL,
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uq_discount_order (discount_id, order_id),
    KEY idx_discount_redemptions_discount (discount_id),
    KEY idx_discount_redemptions_customer (discount_id, customer_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE orders
    ADD COLUMN cart_discount_id VARCHAR(64) NULL DEFAULT NULL AFTER HT,
    ADD COLUMN cart_discount_code VARCHAR(64) NULL DEFAULT NULL AFTER cart_discount_id,
    ADD COLUMN cart_discount_amount INT NOT NULL DEFAULT 0 AFTER cart_discount_code;
