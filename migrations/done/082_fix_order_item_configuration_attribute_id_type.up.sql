-- PostgreSQL migration : corrige le type de
-- order_item_configuration.configuration_attribute_id, de integer vers
-- varchar(64), pour l'aligner sur configurable_attributes.id (varchar(64),
-- IDs prefixes applicatifs, ex. 'attribute-4131d883-3370-426e-9f2a-7ed898fc2021').
--
-- Pourquoi : incoherence de schema preexistante, deja presente dans le DDL
-- MySQL d'origine (configuration_attribute_id y etait deja int(11) face a
-- configurable_attributes.id varchar(64) — voir
-- docs/migration-postgres/15-fk-type-mismatch-audit.md §1.1, et le commentaire
-- laisse en connaissance de cause dans
-- internal/modules/stocks/postgres_integration_test.go). En MySQL non-strict,
-- inserer la vraie valeur ('attribute-xxxx') tronquait silencieusement la
-- colonne a 0. En Postgres la meme insertion leve une erreur dure (22P02,
-- invalid input syntax for type integer), qui fait echouer en production
-- POST /orders/create des qu'un produit commande a une configuration
-- (attributs/options) — cf. BulkInsertConfigs et le bulkInsert
-- "order_item_configuration" dans internal/modules/order_life_cycle/repository.go,
-- appeles depuis CreateOrder et UpdateOrderItem.
--
-- Les valeurs existantes (garbage "0" issu de la troncature MySQL) sont
-- converties telles quelles en '0' : ce n'etait deja pas une donnee
-- exploitable, et la relation reelle order_item -> attribut reste disponible
-- via configuration_attribute_option_id -> configurable_attribute_options
-- .configurable_attribute_id (qui, lui, est correctement typé varchar(64)).
--
-- Un cast integer -> varchar necessite un USING explicite : Postgres n'a pas
-- de cast implicite/assignment enregistré entre integer et varchar.
--
-- La colonne n'est jamais lue par le code Go (uniquement écrite à
-- l'INSERT) : aucun autre changement applicatif requis.

ALTER TABLE order_item_configuration
    ALTER COLUMN configuration_attribute_id TYPE varchar(64)
    USING configuration_attribute_id::varchar(64);

COMMENT ON COLUMN order_item_configuration.configuration_attribute_id IS 'ID prefixe applicatif (configurable_attributes.id, varchar(64)). Corrige de integer vers varchar(64) en migration 082 — voir docs/migration-postgres/15-fk-type-mismatch-audit.md §1.1.';
