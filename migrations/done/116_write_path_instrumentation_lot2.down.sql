-- Reverts 116_write_path_instrumentation_lot2.up.sql.
--
-- deletion_reason_id is narrowed back to varchar(11) only if no existing
-- value would be truncated by doing so (defensive: this migration is additive,
-- but a down migration run long after new, wider values have accumulated
-- must not silently truncate real data).

ALTER TABLE extra
    DROP CONSTRAINT IF EXISTS chk_extra_cost_price_reason;
ALTER TABLE extra
    DROP COLUMN IF EXISTS cost_price_unit,
    DROP COLUMN IF EXISTS cost_price_reason;

ALTER TABLE order_item_configuration
    DROP CONSTRAINT IF EXISTS chk_oic_cost_price_reason;
ALTER TABLE order_item_configuration
    DROP COLUMN IF EXISTS cost_price_unit,
    DROP COLUMN IF EXISTS cost_price_reason;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM orders WHERE length(deletion_reason_id) > 11) THEN
        RAISE EXCEPTION 'orders.deletion_reason_id: cannot narrow back to varchar(11), values longer than 11 characters exist';
    END IF;
    EXECUTE 'ALTER TABLE orders ALTER COLUMN deletion_reason_id TYPE varchar(11)';
END $$;
