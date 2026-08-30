-- PostgreSQL migration : tables de correspondance entre les identifiants d'un
-- provider et les composants/catégories de composant crées par l'import de
-- produits en masse.
--
-- Complète 080_import_provider_mappings.up.sql, qui posait déjà les cinq
-- tables import_products_mapping / import_categories_mapping / import_tags_mapping /
-- import_attributes_mapping / import_attribute_options_mapping : la porte
-- "autre établissement" (copie du catalogue d'un marchand vers un autre) est
-- la première source à créer des composants et des catégories de composant,
-- elle a donc besoin des deux tables symétriques manquantes. Même patron
-- exactement : une table par entité (pas une table polymorphe unique),
-- wello_id typé sur la vraie PK de la table cible, unicité
-- (merchant_id, provider, external_id) pour l'idempotence, index inverse sur
-- wello_id.
--
-- Aucune FK creee (convention du chantier de migration, cf.
-- docs/migration-postgres/04-schema-postgres-target.sql).
-- FK candidates (non creees) :
--   import_component_categories_mapping.wello_id -> component_category.id
--   import_components_mapping.wello_id            -> components.component_id
--   <toutes>.merchant_id                           -> merchant.id
CREATE TABLE IF NOT EXISTS import_component_categories_mapping (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    provider varchar(32) NOT NULL,
    external_id varchar(64) NOT NULL,
    wello_id integer NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    deletion_date timestamptz,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_import_component_categories_mapping_provider_external
    ON import_component_categories_mapping (merchant_id, provider, external_id);

CREATE INDEX IF NOT EXISTS idx_import_component_categories_mapping_wello_id
    ON import_component_categories_mapping (merchant_id, wello_id);

CREATE TABLE IF NOT EXISTS import_components_mapping (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    provider varchar(32) NOT NULL,
    external_id varchar(64) NOT NULL,
    wello_id integer NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    deletion_date timestamptz,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_import_components_mapping_provider_external
    ON import_components_mapping (merchant_id, provider, external_id);

CREATE INDEX IF NOT EXISTS idx_import_components_mapping_wello_id
    ON import_components_mapping (merchant_id, wello_id);

-- ---------------------------------------------------------------------
-- Commentaires
-- ---------------------------------------------------------------------
COMMENT ON TABLE import_component_categories_mapping IS 'Correspondance identifiant provider externe -> component_category.id, posee par l''import de produits en masse (porte "autre établissement").';
COMMENT ON TABLE import_components_mapping IS 'Correspondance identifiant provider externe -> components.component_id, posee par l''import de produits en masse (porte "autre établissement").';

COMMENT ON COLUMN import_component_categories_mapping.provider IS 'Source de l''import. Pour la porte "autre établissement" : "merchant-<source_merchant_id>", namespacé par marchand source pour que deux sources différentes ne collisionnent jamais sur le même external_id.';
COMMENT ON COLUMN import_component_categories_mapping.external_id IS 'component_category.id du marchand source, tel que lu par BuildMerchantCanonicalImport.';
COMMENT ON COLUMN import_component_categories_mapping.wello_id IS 'component_category.id (integer) chez le marchand destination. Pas de FK (convention du dépôt).';
COMMENT ON COLUMN import_components_mapping.external_id IS 'components.component_id du marchand source, tel que lu par BuildMerchantCanonicalImport.';
COMMENT ON COLUMN import_components_mapping.wello_id IS 'components.component_id (integer) chez le marchand destination. Pas de FK (convention du dépôt).';

COMMENT ON INDEX uq_import_component_categories_mapping_provider_external IS 'Clé d''idempotence de l''import. Volontairement sans filtre sur enabled : même sémantique que uq_import_categories_mapping_provider_external (migration 080).';
COMMENT ON INDEX uq_import_components_mapping_provider_external IS 'Clé d''idempotence de l''import. Volontairement sans filtre sur enabled : même sémantique que uq_import_categories_mapping_provider_external (migration 080).';
