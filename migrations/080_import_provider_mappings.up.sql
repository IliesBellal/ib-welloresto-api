-- PostgreSQL migration : tables de correspondance entre les identifiants d'un
-- provider externe (Zelty en premier, d'autres ensuite) et les entites
-- WelloResto creees par l'import de produits en masse.
--
-- Pourquoi des tables dediees plutot que des colonnes sur les entites : aucune
-- des tables cibles (products, productcateg, tags, configurable_attributes,
-- configurable_attribute_options) ne porte de colonne provider/external_id, et
-- les elargir imposerait une migration sur des tables chaudes du menu pour un
-- besoin qui ne concerne que le chemin d'import. Le decouplage suit le patron
-- deja en place pour les plateformes de livraison (integration_uber_eats_*_mapping
-- et integration_deliveroo_*_mapping, cf.
-- docs/migration-postgres/04-schema-postgres-target.sql), a ceci pres que
-- wello_id est ici type sur la vraie PK de la table cible au lieu du varchar(50)
-- indifferencie de ces tables historiques.
--
-- Ce que ces tables rendent possible, et qui est impossible aujourd'hui :
--   - idempotence : re-importer le meme fichier ne recree pas les memes entites,
--     l'unicite (merchant_id, provider, external_id) sert de garde ;
--   - rollback d'un import : retrouver les entites creees par un import donne ;
--   - lookup inverse (index sur wello_id) : savoir d'ou vient une entite.
--
-- Une table par entite plutot qu'une table polymorphe unique : c'est ce que
-- fait le patron integration_*, et cela permet de typer wello_id correctement
-- (integer vs varchar selon la cible) au lieu de tout stocker en texte.
--
-- Aucune FK creee (convention du chantier de migration, cf.
-- docs/migration-postgres/04-schema-postgres-target.sql).
-- FK candidates (non creees) :
--   import_products_mapping.wello_id           -> products.product_id
--   import_categories_mapping.wello_id         -> productcateg.categ_id
--   import_tags_mapping.wello_id               -> tags.tag_id
--   import_attributes_mapping.wello_id         -> configurable_attributes.id
--   import_attribute_options_mapping.wello_id  -> configurable_attribute_options.id
--   <toutes>.merchant_id                       -> merchant.id

-- ---------------------------------------------------------------------
-- Produits : wello_id = products.product_id (integer identity).
-- products a une PK composite (product_id, merchant_Id) ; merchant_id etant
-- deja une colonne de cette table, le couple (merchant_id, wello_id) suffit a
-- designer la ligne cible.
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS import_products_mapping (
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

CREATE UNIQUE INDEX IF NOT EXISTS uq_import_products_mapping_provider_external
    ON import_products_mapping (merchant_id, provider, external_id);

CREATE INDEX IF NOT EXISTS idx_import_products_mapping_wello_id
    ON import_products_mapping (merchant_id, wello_id);

-- ---------------------------------------------------------------------
-- Categories caisse : wello_id = productcateg.categ_id (integer identity, PK).
-- Attention : ce n'est PAS la valeur par laquelle un produit reference sa
-- categorie - products.category est un varchar(30) qui porte
-- productcateg.merchant_categ_id. On indexe ici la vraie PK ; la resolution
-- vers merchant_categ_id se fait cote applicatif.
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS import_categories_mapping (
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

CREATE UNIQUE INDEX IF NOT EXISTS uq_import_categories_mapping_provider_external
    ON import_categories_mapping (merchant_id, provider, external_id);

CREATE INDEX IF NOT EXISTS idx_import_categories_mapping_wello_id
    ON import_categories_mapping (merchant_id, wello_id);

-- ---------------------------------------------------------------------
-- Tags : wello_id = tags.tag_id (varchar(42), ID prefixe "tag-<uuid>").
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS import_tags_mapping (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    provider varchar(32) NOT NULL,
    external_id varchar(64) NOT NULL,
    wello_id varchar(42) NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    deletion_date timestamptz,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_import_tags_mapping_provider_external
    ON import_tags_mapping (merchant_id, provider, external_id);

CREATE INDEX IF NOT EXISTS idx_import_tags_mapping_wello_id
    ON import_tags_mapping (merchant_id, wello_id);

-- ---------------------------------------------------------------------
-- Attributs configurables : wello_id = configurable_attributes.id
-- (varchar(64), ID prefixe "attribute-<uuid>").
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS import_attributes_mapping (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    provider varchar(32) NOT NULL,
    external_id varchar(64) NOT NULL,
    wello_id varchar(64) NOT NULL,
    creation_date timestamptz NOT NULL DEFAULT now(),
    deletion_date timestamptz,
    enabled boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_import_attributes_mapping_provider_external
    ON import_attributes_mapping (merchant_id, provider, external_id);

CREATE INDEX IF NOT EXISTS idx_import_attributes_mapping_wello_id
    ON import_attributes_mapping (merchant_id, wello_id);

-- ---------------------------------------------------------------------
-- Options d'attribut : wello_id = configurable_attribute_options.id
-- (integer identity ; la table ne porte pas de merchant_id, le scoping passe
-- par configurable_attributes).
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS import_attribute_options_mapping (
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

CREATE UNIQUE INDEX IF NOT EXISTS uq_import_attribute_options_mapping_provider_external
    ON import_attribute_options_mapping (merchant_id, provider, external_id);

CREATE INDEX IF NOT EXISTS idx_import_attribute_options_mapping_wello_id
    ON import_attribute_options_mapping (merchant_id, wello_id);

-- ---------------------------------------------------------------------
-- Commentaires
-- ---------------------------------------------------------------------
COMMENT ON TABLE import_products_mapping IS 'Correspondance identifiant provider externe -> products.product_id, posee par l''import de produits en masse.';
COMMENT ON TABLE import_categories_mapping IS 'Correspondance identifiant provider externe -> productcateg.categ_id, posee par l''import de produits en masse.';
COMMENT ON TABLE import_tags_mapping IS 'Correspondance identifiant provider externe -> tags.tag_id, posee par l''import de produits en masse.';
COMMENT ON TABLE import_attributes_mapping IS 'Correspondance identifiant provider externe -> configurable_attributes.id, posee par l''import de produits en masse.';
COMMENT ON TABLE import_attribute_options_mapping IS 'Correspondance identifiant provider externe -> configurable_attribute_options.id, posee par l''import de produits en masse.';

COMMENT ON COLUMN import_products_mapping.provider IS 'Source de l''import : ''zelty'', ''wello-generic''... Fait partie de la cle d''idempotence, deux providers pouvant emettre le meme external_id.';
COMMENT ON COLUMN import_products_mapping.external_id IS 'Identifiant de l''entite chez le provider (ex. Zelty : ZD1557688).';
COMMENT ON COLUMN import_products_mapping.wello_id IS 'products.product_id (integer). Pas de FK (convention du depot).';
COMMENT ON COLUMN import_products_mapping.deletion_date IS 'Horodatage de desactivation logique du mapping. NULL tant que le mapping est actif ; complete enabled, sur le modele des tables integration_*_mapping.';

COMMENT ON COLUMN import_categories_mapping.wello_id IS 'productcateg.categ_id (integer, PK) - et NON productcateg.merchant_categ_id, qui est la valeur varchar(20) par laquelle products.category reference la categorie. La resolution categ_id -> merchant_categ_id est a la charge de l''applicatif.';
COMMENT ON COLUMN import_tags_mapping.wello_id IS 'tags.tag_id (varchar(42), ID prefixe). Pas de FK (convention du depot).';
COMMENT ON COLUMN import_attributes_mapping.wello_id IS 'configurable_attributes.id (varchar(64), ID prefixe). Pas de FK (convention du depot).';
COMMENT ON COLUMN import_attribute_options_mapping.wello_id IS 'configurable_attribute_options.id (integer identity). Pas de FK (convention du depot).';

COMMENT ON INDEX uq_import_products_mapping_provider_external IS 'Cle d''idempotence de l''import. Volontairement sans filtre sur enabled : un mapping desactive continue de bloquer un re-import du meme external_id, ce qui evite de recreer un doublon d''une entite supprimee cote Wello. Reactiver le mapping est une decision explicite.';
