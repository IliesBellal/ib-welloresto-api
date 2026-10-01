-- LOT A Semaine 2, Chantier 6a (docs/decisions.md) : signup_sessions.
--
-- Double rôle documenté dans docs/decisions.md (le brief ne détaille pas le
-- mécanisme d'idempotence) :
--   1. Clé d'idempotence de POST /v1/signup — id = la valeur de l'en-tête
--      Idempotency-Key telle quelle (pas un id préfixé généré côté serveur),
--      pour qu'un rejeu avec la même clé retrouve la même ligne directement.
--      state suit pending -> completed|failed ; payload porte la réponse
--      HTTP mise en cache (rejouée à l'identique 24h, expires_at le borne).
--   2. Support d'un futur contexte pré-signup (context_token) — non construit
--      par ce chantier (aucun endpoint n'écrit une ligne "contexte" séparée),
--      mais la colonne existe pour qu'un chantier ultérieur (page de
--      sélection d'offre, etc.) puisse en écrire une, que /v1/signup lira
--      pour en dériver notamment package_id.
CREATE TABLE signup_sessions (
    id            text PRIMARY KEY,
    email         text,
    provider      text NOT NULL,
    state         text NOT NULL DEFAULT 'pending',
    context_token text,
    payload       jsonb,
    expires_at    timestamptz NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
COMMENT ON COLUMN signup_sessions.id IS 'Valeur de l''en-tête Idempotency-Key de POST /v1/signup, utilisée telle quelle comme clé primaire.';
COMMENT ON COLUMN signup_sessions.state IS 'pending (en cours de traitement) | completed | failed — completed/failed portent la réponse HTTP à rejouer dans payload.';
COMMENT ON COLUMN signup_sessions.expires_at IS 'Fin de la fenêtre de rejeu idempotent (24h après création) — distinct de la purge de rétention à 30 jours (voir internal/tasks).';

CREATE INDEX idx_signup_sessions_created_at ON signup_sessions (created_at);
