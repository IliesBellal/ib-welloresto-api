ALTER TABLE orders
    DROP COLUMN cart_discount_amount,
    DROP COLUMN cart_discount_code,
    DROP COLUMN cart_discount_id;

DROP TABLE IF EXISTS discount_redemptions;

ALTER TABLE discounts
    DROP COLUMN max_redemptions_per_customer,
    DROP COLUMN max_redemptions,
    DROP COLUMN discount_scope;
