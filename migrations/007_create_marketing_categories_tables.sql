CREATE TABLE IF NOT EXISTS marketing_categories (
    id BIGINT NOT NULL AUTO_INCREMENT,
    merchant_id VARCHAR(64) NOT NULL,
    name VARCHAR(191) NOT NULL,
    display_order INT NOT NULL DEFAULT 0,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    available TINYINT(1) NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uq_marketing_categories_merchant_name (merchant_id, name),
    KEY idx_marketing_categories_merchant_enabled_order (merchant_id, enabled, display_order)
);

CREATE TABLE IF NOT EXISTS product_marketing_categories (
    product_id VARCHAR(64) NOT NULL,
    marketing_category_id BIGINT NOT NULL,
    merchant_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (product_id),
    KEY idx_product_marketing_categories_merchant_category (merchant_id, marketing_category_id),
    CONSTRAINT fk_product_marketing_categories_category
        FOREIGN KEY (marketing_category_id) REFERENCES marketing_categories(id)
        ON UPDATE CASCADE
);