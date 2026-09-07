-- CONCURRENTLY requis aussi côté suppression pour la même raison qu'à la
-- création (pas de verrou bloquant sur api_request_logs). Hors transaction.
DROP INDEX CONCURRENTLY IF EXISTS idx_api_request_logs_created_at;
