-- LOT A Semaine 2, Chantier 7a (docs/decisions.md) : rattachement Google.
-- users.auth_provider existe déjà depuis le chantier 6 (migration 128,
-- tirée en avance) ; il ne reste que google_sub à ajouter ici.
--
-- UNIQUE (pas un index partiel) : suffisant tel quel en Postgres, où
-- plusieurs NULL ne sont jamais considérés en conflit par une contrainte
-- UNIQUE — tous les comptes sans Google (google_sub NULL) restent donc
-- acceptés, seul un même google_sub réel ne peut jamais être rattaché deux
-- fois. Reprend exactement le schéma du brief, rien de plus.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS google_sub text;

ALTER TABLE users
    ADD CONSTRAINT uq_users_google_sub UNIQUE (google_sub);

COMMENT ON COLUMN users.google_sub IS 'Identifiant Google (claim "sub" du id_token), NULL tant qu''aucun rattachement Google n''a eu lieu.';
