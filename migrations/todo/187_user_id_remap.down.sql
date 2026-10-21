-- Rollback de 187_user_id_remap.up.sql.
-- À jouer APRÈS le retrait du code qui lit la table (internal/userids,
-- cash_registers, orders_fetcher_builder), et JAMAIS une fois
-- cmd/renumber_users --apply passé : les paiements signés portant un ancien id
-- n'auraient plus aucun lien vers leur utilisateur.

DROP TABLE IF EXISTS user_id_remap;
