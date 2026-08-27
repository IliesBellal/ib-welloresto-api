-- PostgreSQL migration: table des demandes de reinitialisation de mot de passe
-- ("Mot de passe oublie"). Source de verite de la validite et de l'usage unique
-- d'un lien de reinitialisation.
--
-- Pourquoi en base et non en Redis : le client Redis du projet est best-effort
-- (internal/infrastructure/redis/client.go — Get avale les erreurs et renvoie
-- "non trouve", Delete renvoie true meme en echec). Un token stocke en Redis
-- serait donc (1) injoignable en cas de panne Redis, et (2) rejouable si le
-- Delete de consommation echoue silencieusement. La consommation se fait ici
-- par UPDATE ... WHERE used_at IS NULL AND expires_at > now() + controle de
-- RowsAffected = 1, ce qui garantit l'usage unique meme en acces concurrent.
--
-- Aucune FK creee (convention du chantier de migration, cf.
-- docs/migration-postgres/04-schema-postgres-target.sql).
-- FK candidate (non creee) : user_id -> users.user_id

CREATE TABLE IF NOT EXISTS password_resets (
    id varchar(64) NOT NULL,
    user_id varchar(64) NOT NULL,
    token_hash varchar(64) NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    requested_ip varchar(45),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

-- Lookup de consommation du token (un seul acces par appel a /auth/reset-password).
CREATE UNIQUE INDEX IF NOT EXISTS uq_password_resets_token_hash
    ON password_resets (token_hash);

-- Rate limit par compte : COUNT(*) des demandes d'un user sur la derniere heure.
CREATE INDEX IF NOT EXISTS idx_password_resets_user_created
    ON password_resets (user_id, created_at);

-- Purge quotidienne (DELETE ... WHERE created_at < now() - interval '7 days').
-- Indexe car le pool est plafonne a 1 connexion (internal/database/postgres.go) :
-- un seq scan sur la purge bloquerait l'unique connexion de l'API.
CREATE INDEX IF NOT EXISTS idx_password_resets_created_at
    ON password_resets (created_at);

COMMENT ON TABLE password_resets IS 'Demandes de reinitialisation de mot de passe. Le token n''est jamais stocke en clair : token_hash = sha256 hex (64 caracteres) du token envoye par email.';
COMMENT ON COLUMN password_resets.token_hash IS 'sha256 hex du token en clair. Unique : sert de cle de lookup lors de la consommation.';
COMMENT ON COLUMN password_resets.used_at IS 'NULL tant que le token n''a pas ete consomme. Passe a now() par l''UPDATE atomique de consommation — garantit l''usage unique.';
COMMENT ON COLUMN password_resets.requested_ip IS 'IP de la demande (45 caracteres = IPv6 max). Tracabilite et rate limit par IP en secours si Redis est indisponible.';
