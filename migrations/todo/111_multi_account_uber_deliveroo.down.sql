-- Rollback de 111_multi_account_uber_deliveroo.up.sql.
--
-- Le retour à PRIMARY KEY (merchant_id) seul échouera si un marchand a déjà
-- plus d'un compte Uber Eats ou Deliveroo au moment du rollback (violation
-- d'unicité) : à n'utiliser qu'en rollback immédiat, avant toute création
-- effective d'un deuxième compte.

-- ---------------------------------------------------------------------------
-- 4. orders
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_orders_brand_store_id;
ALTER TABLE orders DROP COLUMN IF EXISTS brand_store_id;

-- ---------------------------------------------------------------------------
-- 3. Mappings menu Deliveroo
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_idr_components_mapping_location_id;
DROP INDEX IF EXISTS idx_idr_attributes_mapping_location_id;
DROP INDEX IF EXISTS idx_idr_options_mapping_location_id;
DROP INDEX IF EXISTS idx_idr_products_mapping_location_id;

ALTER TABLE integration_deliveroo_components_mapping DROP COLUMN IF EXISTS location_id;
ALTER TABLE integration_deliveroo_attributes_mapping DROP COLUMN IF EXISTS location_id;
ALTER TABLE integration_deliveroo_options_mapping DROP COLUMN IF EXISTS location_id;
ALTER TABLE integration_deliveroo_products_mapping DROP COLUMN IF EXISTS location_id;

-- ---------------------------------------------------------------------------
-- 2. Mappings menu Uber Eats
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_iue_components_mapping_store_id;
DROP INDEX IF EXISTS idx_iue_attributes_mapping_store_id;
DROP INDEX IF EXISTS idx_iue_options_mapping_store_id;
DROP INDEX IF EXISTS idx_iue_products_mapping_store_id;

ALTER TABLE integration_uber_eats_components_mapping DROP COLUMN IF EXISTS store_id;
ALTER TABLE integration_uber_eats_attributes_mapping DROP COLUMN IF EXISTS store_id;
ALTER TABLE integration_uber_eats_options_mapping DROP COLUMN IF EXISTS store_id;
ALTER TABLE integration_uber_eats_products_mapping DROP COLUMN IF EXISTS store_id;

-- ---------------------------------------------------------------------------
-- 1. integration_uber_eats / integration_deliveroo
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_integration_deliveroo_location_id;
ALTER TABLE integration_deliveroo DROP CONSTRAINT integration_deliveroo_pkey;
ALTER TABLE integration_deliveroo ADD PRIMARY KEY (merchant_id);

DROP INDEX IF EXISTS idx_integration_uber_eats_store_id;
ALTER TABLE integration_uber_eats DROP CONSTRAINT integration_uber_eats_pkey;
ALTER TABLE integration_uber_eats ADD PRIMARY KEY (merchant_id);
