-- Rollback de 084_import_customers_mapping.up.sql
-- Les index (ux_* et idx_*) sont supprimes avec leur table (pas de DROP INDEX
-- explicite necessaire).

DROP TABLE IF EXISTS import_customers_mapping;
