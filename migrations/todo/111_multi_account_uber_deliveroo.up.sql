-- PostgreSQL migration : prépare integration_uber_eats / integration_deliveroo et
-- leurs tables satellites (mappings menu, orders) au support de plusieurs comptes
-- Uber Eats / Deliveroo par marchand.
--
-- Contexte : les deux tables d'intégration ont aujourd'hui PRIMARY KEY (merchant_id)
-- seul, ce qui interdit plus d'un compte par marchand. store_id (Uber) et location_id
-- (Deliveroo) sont déjà des colonnes NOT NULL de ces tables et jouent déjà le rôle de
-- clé de routage webhook (UberRepository.GetMerchantIDFromStoreID,
-- DeliverooRepository.GetSiteIDByMerchant) : on les promeut en second membre de la
-- clé primaire plutôt que de créer une table séparée.
--
-- Trois familles de tables concernées :
--   1. integration_uber_eats / integration_deliveroo : PK composite
--      (merchant_id, store_id) / (merchant_id, location_id).
--   2. Tables de mapping menu (produits/options/attributs/composants) : ajout de
--      store_id / location_id pour qu'un deuxième compte du même marchand ne
--      partage pas les mappings du premier.
--   3. orders : ajout de store_id / location_id pour tracer, commande par
--      commande, le compte d'origine (déjà indispensable côté webhook, qui reçoit
--      ces IDs à chaque notification).
--
-- Aucune FK créée (convention du chantier de migration MySQL -> Postgres pour ce
-- périmètre de tables legacy, cf. docs/migration-postgres/04-schema-postgres-target.sql).
--
-- store_id / location_id restent NULLABLE sur les tables de mapping et sur orders :
-- une ligne orpheline (mapping ou commande sans intégration correspondante encore
-- en base au moment de la migration) ne doit jamais faire échouer le déploiement.
-- Le code applicatif (lot backend associé) renseigne systématiquement la colonne
-- pour toute nouvelle ligne à partir de cette migration.
--
-- Ce que cette migration NE fait PAS : elle ne touche pas aux contraintes UNIQUE
-- existantes des tables de mapping (ex. uq_integration_uber_eats_options_mapping_
-- unique_mapping sur (merchant_id, item_id)) ni au code Go qui s'appuie dessus
-- (ON CONFLICT / ON DUPLICATE KEY). Les rendre uniques par compte est un chantier
-- séparé, hors scope ici.

-- ---------------------------------------------------------------------------
-- 1. integration_uber_eats / integration_deliveroo : clé composite par compte
-- ---------------------------------------------------------------------------
-- Sans risque de collision : une seule ligne par merchant_id existe aujourd'hui
-- (c'était la PK), donc (merchant_id, store_id) / (merchant_id, location_id) est
-- garanti unique au moment de cette migration.
ALTER TABLE integration_uber_eats DROP CONSTRAINT integration_uber_eats_pkey;
ALTER TABLE integration_uber_eats ADD PRIMARY KEY (merchant_id, store_id);
CREATE INDEX IF NOT EXISTS idx_integration_uber_eats_store_id
    ON integration_uber_eats (store_id);

ALTER TABLE integration_deliveroo DROP CONSTRAINT integration_deliveroo_pkey;
ALTER TABLE integration_deliveroo ADD PRIMARY KEY (merchant_id, location_id);
CREATE INDEX IF NOT EXISTS idx_integration_deliveroo_location_id
    ON integration_deliveroo (location_id);

-- ---------------------------------------------------------------------------
-- 2. Mappings menu Uber Eats : store_id + backfill depuis l'intégration actuelle
-- ---------------------------------------------------------------------------
ALTER TABLE integration_uber_eats_products_mapping ADD COLUMN IF NOT EXISTS store_id varchar(150);
ALTER TABLE integration_uber_eats_options_mapping ADD COLUMN IF NOT EXISTS store_id varchar(150);
ALTER TABLE integration_uber_eats_attributes_mapping ADD COLUMN IF NOT EXISTS store_id varchar(150);
ALTER TABLE integration_uber_eats_components_mapping ADD COLUMN IF NOT EXISTS store_id varchar(150);

-- Backfill : chaque marchand n'ayant qu'un compte Uber Eats à ce jour, le
-- rattachement via merchant_id est sans ambiguïté.
UPDATE integration_uber_eats_products_mapping m
SET store_id = iue.store_id
FROM integration_uber_eats iue
WHERE iue.merchant_id = m.merchant_id AND m.store_id IS NULL;

UPDATE integration_uber_eats_options_mapping m
SET store_id = iue.store_id
FROM integration_uber_eats iue
WHERE iue.merchant_id = m.merchant_id AND m.store_id IS NULL;

UPDATE integration_uber_eats_attributes_mapping m
SET store_id = iue.store_id
FROM integration_uber_eats iue
WHERE iue.merchant_id = m.merchant_id AND m.store_id IS NULL;

UPDATE integration_uber_eats_components_mapping m
SET store_id = iue.store_id
FROM integration_uber_eats iue
WHERE iue.merchant_id = m.merchant_id AND m.store_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_iue_products_mapping_store_id
    ON integration_uber_eats_products_mapping (merchant_id, store_id);
CREATE INDEX IF NOT EXISTS idx_iue_options_mapping_store_id
    ON integration_uber_eats_options_mapping (merchant_id, store_id);
CREATE INDEX IF NOT EXISTS idx_iue_attributes_mapping_store_id
    ON integration_uber_eats_attributes_mapping (merchant_id, store_id);
CREATE INDEX IF NOT EXISTS idx_iue_components_mapping_store_id
    ON integration_uber_eats_components_mapping (merchant_id, store_id);

COMMENT ON COLUMN integration_uber_eats_products_mapping.store_id IS 'Compte Uber Eats propriétaire du mapping (integration_uber_eats.store_id). NULL seulement pour une ligne orpheline (marchand sans intégration en base au moment de la migration 111).';
COMMENT ON COLUMN integration_uber_eats_options_mapping.store_id IS 'Compte Uber Eats propriétaire du mapping (integration_uber_eats.store_id). NULL seulement pour une ligne orpheline (marchand sans intégration en base au moment de la migration 111).';
COMMENT ON COLUMN integration_uber_eats_attributes_mapping.store_id IS 'Compte Uber Eats propriétaire du mapping (integration_uber_eats.store_id). NULL seulement pour une ligne orpheline (marchand sans intégration en base au moment de la migration 111).';
COMMENT ON COLUMN integration_uber_eats_components_mapping.store_id IS 'Compte Uber Eats propriétaire du mapping (integration_uber_eats.store_id). NULL seulement pour une ligne orpheline (marchand sans intégration en base au moment de la migration 111).';

-- ---------------------------------------------------------------------------
-- 3. Mappings menu Deliveroo : location_id + backfill depuis l'intégration actuelle
-- ---------------------------------------------------------------------------
ALTER TABLE integration_deliveroo_products_mapping ADD COLUMN IF NOT EXISTS location_id varchar(20);
ALTER TABLE integration_deliveroo_options_mapping ADD COLUMN IF NOT EXISTS location_id varchar(20);
ALTER TABLE integration_deliveroo_attributes_mapping ADD COLUMN IF NOT EXISTS location_id varchar(20);
ALTER TABLE integration_deliveroo_components_mapping ADD COLUMN IF NOT EXISTS location_id varchar(20);

UPDATE integration_deliveroo_products_mapping m
SET location_id = idr.location_id
FROM integration_deliveroo idr
WHERE idr.merchant_id = m.merchant_id AND m.location_id IS NULL;

UPDATE integration_deliveroo_options_mapping m
SET location_id = idr.location_id
FROM integration_deliveroo idr
WHERE idr.merchant_id = m.merchant_id AND m.location_id IS NULL;

UPDATE integration_deliveroo_attributes_mapping m
SET location_id = idr.location_id
FROM integration_deliveroo idr
WHERE idr.merchant_id = m.merchant_id AND m.location_id IS NULL;

UPDATE integration_deliveroo_components_mapping m
SET location_id = idr.location_id
FROM integration_deliveroo idr
WHERE idr.merchant_id = m.merchant_id AND m.location_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_idr_products_mapping_location_id
    ON integration_deliveroo_products_mapping (merchant_id, location_id);
CREATE INDEX IF NOT EXISTS idx_idr_options_mapping_location_id
    ON integration_deliveroo_options_mapping (merchant_id, location_id);
CREATE INDEX IF NOT EXISTS idx_idr_attributes_mapping_location_id
    ON integration_deliveroo_attributes_mapping (merchant_id, location_id);
CREATE INDEX IF NOT EXISTS idx_idr_components_mapping_location_id
    ON integration_deliveroo_components_mapping (merchant_id, location_id);

COMMENT ON COLUMN integration_deliveroo_products_mapping.location_id IS 'Compte Deliveroo propriétaire du mapping (integration_deliveroo.location_id). NULL seulement pour une ligne orpheline (marchand sans intégration en base au moment de la migration 111).';
COMMENT ON COLUMN integration_deliveroo_options_mapping.location_id IS 'Compte Deliveroo propriétaire du mapping (integration_deliveroo.location_id). NULL seulement pour une ligne orpheline (marchand sans intégration en base au moment de la migration 111).';
COMMENT ON COLUMN integration_deliveroo_attributes_mapping.location_id IS 'Compte Deliveroo propriétaire du mapping (integration_deliveroo.location_id). NULL seulement pour une ligne orpheline (marchand sans intégration en base au moment de la migration 111).';
COMMENT ON COLUMN integration_deliveroo_components_mapping.location_id IS 'Compte Deliveroo propriétaire du mapping (integration_deliveroo.location_id). NULL seulement pour une ligne orpheline (marchand sans intégration en base au moment de la migration 111).';

-- ---------------------------------------------------------------------------
-- 4. orders : traçabilité du compte d'origine
-- ---------------------------------------------------------------------------
-- Un seul champ brand_store_id (plutôt que store_id + location_id) : orders.
-- brand désambiguïse déjà de quel provider il s'agit, donc pas besoin de deux
-- colonnes pour porter le même concept ("compte d'origine chez ce provider").
-- Contrairement aux tables de mapping menu (section 2/3), qui sont déjà
-- séparées par provider via leur nom de table et gardent donc store_id /
-- location_id distincts sans que ça coûte rien.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS brand_store_id varchar(150);

-- Backfill des commandes déjà en base : rattachement par marchand + brand, sans
-- ambiguïté puisqu'un seul compte existe par marchand à ce jour.
UPDATE orders o
SET brand_store_id = iue.store_id
FROM integration_uber_eats iue
WHERE o.brand = 'UBER_EATS' AND o.merchant_id = iue.merchant_id AND o.brand_store_id IS NULL;

UPDATE orders o
SET brand_store_id = idr.location_id
FROM integration_deliveroo idr
WHERE o.brand = 'DELIVEROO' AND o.merchant_id = idr.merchant_id AND o.brand_store_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_orders_brand_store_id
    ON orders (merchant_id, brand, brand_store_id);

COMMENT ON COLUMN orders.brand_store_id IS 'Compte d''origine de la commande chez le provider (integration_uber_eats.store_id quand brand = ''UBER_EATS'', integration_deliveroo.location_id quand brand = ''DELIVEROO''). NULL pour WELLO_RESTO/SCANNORDER et pour les commandes antérieures à la migration 111 sans intégration retrouvée.';
