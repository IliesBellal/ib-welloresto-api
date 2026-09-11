-- LOT A Semaine 2 — colonne users.auth_provider.
--
-- Déclarée sous "Chantier 7a. Schéma" par le brief (avec google_sub), mais
-- requise dès le chantier 6 : CreateUser du parcours /v1/signup (chantier 6b)
-- écrit auth_provider = 'password' immédiatement. Tirée en avance dans ce
-- chantier pour que le code de la semaine compile et s'exécute sans
-- dépendre d'une migration numérotée plus tard ; google_sub reste dans le
-- périmètre du chantier 7 (colonne non ajoutée ici).
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS auth_provider text NOT NULL DEFAULT 'password';

COMMENT ON COLUMN users.auth_provider IS '''password'' ou ''google'' (chantier 7). Tout utilisateur existant avant ce chantier est ''password'' par défaut (comportement inchangé, ils ont tous un mot de passe).';
