-- Index sur api_request_logs.created_at, requis par la purge mensuelle
-- (internal/tasks/request_logs.go, CleanupOldRequestLogs). Sans cet index,
-- chaque lot de la purge ferait un parcours complet de la table (207 218
-- lignes à l'écriture de cette migration, et en croissance depuis l'ajout de
-- response_payload en migration 108 — 5 313 lignes constatées sur staging le
-- 2026-09-07, la purge ayant tourné entre-temps ; ne pas se fier à ce chiffre
-- pour estimer le volume de production, le mesurer séparément) pour trouver
-- les lignes à supprimer.
--
-- La table n'a aujourd'hui aucun index hors sa clé primaire.
--
-- ATTENTION - CE FICHIER NE DOIT PAS ÊTRE EXÉCUTÉ DANS UNE TRANSACTION.
-- Réécrit le 2026-09-07 (PROMPT 27 Phase 1) : le fichier original créait cet
-- index sans CONCURRENTLY, ce qui pose un verrou SHARE sur api_request_logs
-- (bloque les écritures pendant toute la construction) — sans conséquence
-- sur staging (5-207k lignes) mais potentiellement long en production, où le
-- volume réel est inconnu d'ici (docs/migration-postgres/67-migration-status-audit.md
-- §3.2 ligne 2). CREATE INDEX CONCURRENTLY est refusé par PostgreSQL à
-- l'intérieur d'un bloc transactionnel : jouer cette instruction seule, hors
-- BEGIN/COMMIT, comme 087_analytics_indexes.up.sql.
--
-- Si la création échoue en cours de route, l'index reste en état "invalid" :
-- le supprimer (cf. .down.sql) et rejouer, ne jamais laisser un index
-- invalide en place.

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_api_request_logs_created_at
    ON api_request_logs (created_at);

ANALYZE api_request_logs;
