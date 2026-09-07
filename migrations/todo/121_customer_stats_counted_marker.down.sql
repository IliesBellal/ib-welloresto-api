-- Reverts 121_customer_stats_counted_marker.up.sql.
ALTER TABLE orders
    DROP COLUMN IF EXISTS customer_stats_counted_at;
