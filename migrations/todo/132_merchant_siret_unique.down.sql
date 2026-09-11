-- Reverts 132_merchant_siret_unique.up.sql.
-- CONCURRENTLY again: must not run inside a transaction, same as the up file.
DROP INDEX CONCURRENTLY IF EXISTS uq_merchant_siret_valid;
