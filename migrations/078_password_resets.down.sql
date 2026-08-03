-- Rollback de 078_password_resets.up.sql
-- Les index sont supprimes avec la table (pas de DROP INDEX explicite necessaire).

DROP TABLE IF EXISTS password_resets;
