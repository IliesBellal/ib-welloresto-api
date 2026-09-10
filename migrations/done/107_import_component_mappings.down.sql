-- Rollback de 107_import_component_mappings.up.sql
-- Les index (uq_* et idx_*) sont supprimés avec leur table (pas de DROP INDEX
-- explicite nécessaire).

DROP TABLE IF EXISTS import_component_categories_mapping;
DROP TABLE IF EXISTS import_components_mapping;
