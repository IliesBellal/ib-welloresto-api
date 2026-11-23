-- Fiche Google et demandes d'avis Google par e-mail (docs/scannorder-seo.md,
-- « Fiche Google et demandes d'avis »).
--
-- scannorder_settings :
--   google_place_id          identifiant Google du lieu (Place ID), renseigné
--                            quand le restaurateur choisit sa fiche dans la
--                            recherche du back-office. NULL = inconnu.
--   google_maps_url          lien public de la fiche Google : publié en sameAs
--                            et hasMap sur la page ScanNOrder. NULL = aucun.
--   google_review_url        lien « laisser un avis » envoyé aux clients.
--                            Domaine Google uniquement (contrôlé par l'API).
--   review_request_enabled   envoi d'une demande d'avis par e-mail après
--                            chaque commande ScanNOrder close. Faux par défaut.
--
-- review_requests : une ligne par commande considérée, envoyée ou non
-- (anti-doublon, plafond de fréquence, jeton du lien de désinscription).
-- Aucune adresse e-mail en clair : email_hash = sha256 hexadécimal de
-- l'adresse en minuscules, sans espaces.
--   status : pending (envoi en cours ; une ligne restée pending n'est jamais
--   renvoyée), sent, failed (pas de nouvel essai), skipped_opt_out (client
--   désinscrit), skipped_recent (déjà sollicité depuis moins de 90 jours),
--   skipped_invalid (adresse e-mail invalide).
--
-- review_request_optouts : clients qui ne veulent plus de demandes d'avis
-- d'un établissement (lien du pied de l'e-mail). Par établissement.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Rejouable. Colonnes
-- nullables ou avec valeur par défaut constante : instantané.
ALTER TABLE scannorder_settings
  ADD COLUMN IF NOT EXISTS google_place_id varchar(255),
  ADD COLUMN IF NOT EXISTS google_maps_url text,
  ADD COLUMN IF NOT EXISTS google_review_url text,
  ADD COLUMN IF NOT EXISTS review_request_enabled boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN scannorder_settings.google_place_id IS 'Place ID Google de l''établissement (recherche du back-office). NULL = inconnu.';
COMMENT ON COLUMN scannorder_settings.google_maps_url IS 'Lien public de la fiche Google (sameAs, hasMap). NULL = aucun.';
COMMENT ON COLUMN scannorder_settings.google_review_url IS 'Lien « laisser un avis » Google envoyé aux clients. NULL = aucun.';
COMMENT ON COLUMN scannorder_settings.review_request_enabled IS 'Demande d''avis Google par e-mail après chaque commande ScanNOrder close.';

CREATE TABLE IF NOT EXISTS review_requests (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    merchant_id varchar(64) NOT NULL,
    order_id varchar(64) NOT NULL,
    email_hash char(64) NOT NULL,
    token varchar(64) NOT NULL,
    status varchar(20) NOT NULL,
    provider_message_id varchar(255),
    created_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz,
    CONSTRAINT uq_review_requests_order UNIQUE (order_id),
    CONSTRAINT uq_review_requests_token UNIQUE (token),
    CONSTRAINT chk_review_requests_status CHECK (status IN ('pending', 'sent', 'failed', 'skipped_opt_out', 'skipped_recent', 'skipped_invalid'))
);
CREATE INDEX IF NOT EXISTS idx_review_requests_merchant_email ON review_requests (merchant_id, email_hash, created_at);

COMMENT ON TABLE review_requests IS 'Demandes d''avis Google par e-mail, une ligne par commande considérée (docs/scannorder-seo.md).';

CREATE TABLE IF NOT EXISTS review_request_optouts (
    merchant_id varchar(64) NOT NULL,
    email_hash char(64) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (merchant_id, email_hash)
);

COMMENT ON TABLE review_request_optouts IS 'Clients qui ne veulent plus de demandes d''avis d''un établissement.';
