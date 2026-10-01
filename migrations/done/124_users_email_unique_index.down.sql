-- Reverts 124_users_email_unique_index.up.sql.
--
-- Hors bloc transactionnel (symétrique à CREATE INDEX CONCURRENTLY côté up).
-- Ne réécrit pas email/name : la normalisation (casse, espaces,
-- désambiguïsation des doublons) est une correction de données, pas une
-- structure réversible — même convention que le reste de ce dépôt (voir par
-- ex. 117_cleanup_deletion_reason_id_quotes, sans down.sql de données).
DROP INDEX CONCURRENTLY IF EXISTS uq_users_email_lower;
