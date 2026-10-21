-- Table de correspondance ancien id -> nouvel id utilisateur
-- (docs/ids-entiers-users-discounts-chantier.md, étape 3).
--
-- Remplie par cmd/renumber_users --apply, qui renumérote les utilisateurs
-- "user-<uuid>" (créés en Go entre 2026-04 et la migration 186) en ids
-- numériques tirés de users_user_id_seq. Créée vide ici : additive, sans
-- effet tant que la commande n'a pas tourné.
--
-- Jamais vidée ni supprimée une fois remplie, comme
-- discounts.legacy_discount_id : les lignes fiscales déjà signées
-- (payments.user_id, audit_logs.user_id — fiscal/payloads.go) gardent l'ancien
-- id pour toujours, et cette table est le seul lien entre les deux. Elle sert
-- aussi au filet d'entrée (internal/userids.Resolve) : un client encore en
-- circulation qui envoie un "user-<uuid>" est traduit au lieu d'échouer.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Le détail d'une caisse
-- (cash_registers.GetCashRegisterDetail) et la liste des paiements d'une
-- commande joignent cette table ; sans elle, ces lectures échouent.

CREATE TABLE IF NOT EXISTS user_id_remap (
    legacy_user_id varchar(64) PRIMARY KEY,
    user_id        varchar(64) NOT NULL UNIQUE,
    remapped_at    timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE user_id_remap IS 'Ancien users.user_id ("user-<uuid>") -> nouvel id numérique. Remplie par cmd/renumber_users. Ne jamais vider : les lignes fiscales signées gardent l''ancien id.';
