-- Rollback de 080_import_provider_mappings.up.sql
-- Les index (uq_* et idx_*) sont supprimes avec leur table (pas de DROP INDEX
-- explicite necessaire).

DROP TABLE IF EXISTS import_attribute_options_mapping;
DROP TABLE IF EXISTS import_attributes_mapping;
DROP TABLE IF EXISTS import_tags_mapping;
DROP TABLE IF EXISTS import_categories_mapping;
DROP TABLE IF EXISTS import_products_mapping;
