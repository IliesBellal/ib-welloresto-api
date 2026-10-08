-- Reverts 173_order_item_configuration_extra_price.up.sql.
ALTER TABLE order_item_configuration DROP COLUMN IF EXISTS extra_price;
