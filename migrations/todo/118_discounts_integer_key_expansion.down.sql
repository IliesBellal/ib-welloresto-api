-- Reverts 118_discounts_integer_key_expansion.up.sql.
--
-- Safe to run even after discount_redemptions has accumulated real rows
-- (Phase 3/5 write paths) since the values being dropped are all additive
-- from the up migration — no data introduced by this lot lives anywhere
-- else. discounts.legacy_discount_id / discount_id_new are dropped too:
-- unlike the "keep forever" intent stated in the up migration, a down
-- migration is a full revert of THIS lot, not a partial rollback.

ALTER TABLE discount_redemptions ALTER COLUMN scope DROP NOT NULL;

DROP INDEX IF EXISTS uq_discount_redemptions_cart;
DROP INDEX IF EXISTS uq_discount_redemptions_product_line;

-- Restore the original UNIQUE (discount_id, order_id) from 041_cart_discounts,
-- only if no scope=PRODUCT_LINE row (which could legitimately violate it —
-- same discount applied to 2 lines of the same order) has been written since.
DO $$
BEGIN
    IF EXISTS (
        SELECT discount_id, order_id FROM discount_redemptions
        GROUP BY discount_id, order_id HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'discount_redemptions: cannot restore uq_discount_order, duplicate (discount_id, order_id) pairs exist (expected for scope=PRODUCT_LINE rows written since this lot)';
    END IF;
    EXECUTE 'CREATE UNIQUE INDEX uq_discount_redemptions_uq_discount_order ON discount_redemptions (discount_id, order_id)';
END $$;

ALTER TABLE discount_redemptions DROP CONSTRAINT IF EXISTS chk_discount_redemptions_scope_order_item;
ALTER TABLE discount_redemptions DROP CONSTRAINT IF EXISTS fk_discount_redemptions_customer_id;
ALTER TABLE discount_redemptions DROP CONSTRAINT IF EXISTS fk_discount_redemptions_order_item_id;
DROP INDEX IF EXISTS uq_orderitems_order_item_id;
ALTER TABLE discount_redemptions DROP CONSTRAINT IF EXISTS fk_discount_redemptions_order_id;
ALTER TABLE discount_redemptions DROP CONSTRAINT IF EXISTS fk_discount_redemptions_discount_id;

ALTER TABLE discount_redemptions ALTER COLUMN customer_id TYPE varchar(64);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM discount_redemptions) THEN
        RAISE EXCEPTION 'discount_redemptions: cannot narrow discount_id back to varchar(64), rows exist that were written under the integer type';
    END IF;
    EXECUTE 'ALTER TABLE discount_redemptions ALTER COLUMN discount_id TYPE varchar(64) USING discount_id::varchar(64)';
END $$;

ALTER TABLE discount_redemptions
    DROP COLUMN IF EXISTS is_reconstructed,
    DROP COLUMN IF EXISTS order_item_id,
    DROP COLUMN IF EXISTS scope;

DROP TYPE IF EXISTS discount_redemptions_scope_enum;

ALTER TABLE discounts_products_options DROP CONSTRAINT IF EXISTS fk_discounts_products_options_discount_id_new;
ALTER TABLE discounts_products_options DROP COLUMN IF EXISTS discount_id_new;

ALTER TABLE discounts_schedules DROP CONSTRAINT IF EXISTS fk_discounts_schedules_discount_id_new;
ALTER TABLE discounts_schedules DROP COLUMN IF EXISTS discount_id_new;

ALTER TABLE discounts_products DROP CONSTRAINT IF EXISTS fk_discounts_products_discount_id_new;
ALTER TABLE discounts_products DROP COLUMN IF EXISTS discount_id_new;

ALTER TABLE discounts ALTER COLUMN discount_id_new DROP DEFAULT;
DROP SEQUENCE IF EXISTS discounts_discount_id_new_seq;
DROP INDEX IF EXISTS uq_discounts_discount_id_new;
ALTER TABLE discounts
    DROP COLUMN IF EXISTS discount_id_new,
    DROP COLUMN IF EXISTS legacy_discount_id;
