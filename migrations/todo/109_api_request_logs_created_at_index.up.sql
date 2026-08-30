-- Index sur api_request_logs.created_at, requis par la purge mensuelle
-- (internal/tasks/request_logs.go, CleanupOldRequestLogs). Sans cet index,
-- chaque lot de la purge ferait un parcours complet de la table (207 218
-- lignes à l'écriture de cette migration, et en croissance depuis l'ajout de
-- response_payload en migration 108) pour trouver les lignes à supprimer.
--
-- La table n'a aujourd'hui aucun index hors sa clé primaire.

CREATE INDEX IF NOT EXISTS idx_api_request_logs_created_at
    ON api_request_logs (created_at);
