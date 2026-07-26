-- Aligne users.enabled sur le type MySQL idiomatique des ~30 autres colonnes
-- booleennes de la table (isReception, isWaiter, admin, terms_of_use_accepted,
-- toutes tinyint(1)) et sur le plan initial docs/migration-postgres/04-schema-mapping-notes.md
-- (user_status_view attendait deja `u.enabled = false`). La colonne stockait
-- deja exclusivement 0/1 (audit docs/migration-postgres/59-users-enabled-boolean-conversion.md,
-- export reel 46/46 lignes + invariant deja impose par le scan Go vers bool sur
-- le chemin de connexion). MODIFY int(11) -> tinyint(1) ne change aucune donnee.

ALTER TABLE users
  MODIFY enabled tinyint(1) NOT NULL DEFAULT 1;
