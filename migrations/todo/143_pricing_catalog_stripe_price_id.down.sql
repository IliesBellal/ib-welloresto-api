-- Reverts 143_pricing_catalog_stripe_price_id.up.sql.
ALTER TABLE pricing_catalog
    DROP COLUMN IF EXISTS stripe_price_id,
    DROP COLUMN IF EXISTS per_unit_stripe_price_id;
