-- PostgreSQL migration : table de correspondance entre les identifiants d'un
-- provider externe (Zelty en premier, d'autres ensuite) et les clients
-- WelloResto crees par l'import de clients en masse.
--
-- Meme patron que import_products_mapping et les autres tables import_*_mapping
-- posees par l'import de produits (cf. migration 080_import_provider_mappings) :
-- une table dediee plutot que des colonnes sur customer, pour ne pas elargir
-- une table chaude du menu clients pour un besoin qui ne concerne que le
-- chemin d'import.
--
-- Ce que cette table rend possible, et qui est impossible aujourd'hui :
--   - idempotence : re-importer le meme fichier ne recree pas les memes clients,
--     l'unicite (merchant_id, provider, external_id) sert de garde ;
--   - rollback d'un import : retrouver les clients crees par un import donne ;
--   - lookup inverse (index sur wello_id) : savoir d'ou vient un client.
--
-- Aucune FK creee (convention du chantier de migration, cf.
-- docs/migration-postgres/04-schema-postgres-target.sql).
-- FK candidates (non creees) :
--   import_customers_mapping.wello_id       -> customer.customer_id
--   import_customers_mapping.merchant_id    -> merchant.id
CREATE TABLE IF NOT EXISTS import_customers_mapping (
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

CREATE UNIQUE INDEX IF NOT EXISTS ux_import_customers_mapping_ident
    ON import_customers_mapping (merchant_id, provider, external_id);

CREATE INDEX IF NOT EXISTS idx_import_customers_mapping_wello
    ON import_customers_mapping (merchant_id, wello_id);

-- ---------------------------------------------------------------------
-- Commentaires
-- ---------------------------------------------------------------------
COMMENT ON TABLE import_customers_mapping IS 'Correspondance identifiant provider externe -> customer.customer_id, posee par l''import de clients en masse.';

COMMENT ON COLUMN import_customers_mapping.provider IS 'Source de l''import : ''zelty'', ''wello-generic'', ''manual''... Fait partie de la cle d''idempotence, deux providers pouvant emettre le meme external_id.';
COMMENT ON COLUMN import_customers_mapping.external_id IS 'Identifiant du client chez le provider, ou identifiant genere si la source n''en fournit pas.';
COMMENT ON COLUMN import_customers_mapping.wello_id IS 'customer.customer_id (integer). Pas de FK (convention du depot).';
COMMENT ON COLUMN import_customers_mapping.deletion_date IS 'Horodatage de desactivation logique du mapping. NULL tant que le mapping est actif ; complete enabled, sur le modele des tables integration_*_mapping.';

COMMENT ON INDEX ux_import_customers_mapping_ident IS 'Cle d''idempotence de l''import. Volontairement sans filtre sur enabled : un mapping desactive continue de bloquer un re-import du meme external_id, ce qui evite de recreer un doublon d''un client supprime cote Wello. Reactiver le mapping est une decision explicite.';
