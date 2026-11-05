-- Reverts 198_customer_search.up.sql.
--
-- Hors bloc transactionnel (DROP INDEX CONCURRENTLY). À jouer APRÈS avoir
-- redéployé une version du code qui n'appelle plus customer_search_*.
-- Les extensions pg_trgm / unaccent / btree_gin sont laissées en place :
-- d'autres objets de la base peuvent en dépendre.
DROP INDEX CONCURRENTLY IF EXISTS idx_customer_search_email;
DROP INDEX CONCURRENTLY IF EXISTS idx_customer_search_phone;
DROP INDEX CONCURRENTLY IF EXISTS idx_customer_search_name;

DROP FUNCTION IF EXISTS customer_search_phone(text);
DROP FUNCTION IF EXISTS customer_search_name(text, text, text);
DROP FUNCTION IF EXISTS customer_search_norm(text);
