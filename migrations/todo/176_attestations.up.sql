-- Conformité caisse, lot F (docs/attestation-conformite-07-lot-F-brief.md) :
-- attestations individuelles de l'éditeur (modèle BOI-LETTRE-000242), générées
-- par le restaurateur depuis le back-office ou la caisse. Une ligne par
-- attestation, jamais modifiée ni supprimée ; le PDF (volet 1 pré-rempli et
-- pré-signé par l'éditeur, volet 2 complété et signé électroniquement par le
-- représentant légal de l'établissement) est dans le bucket R2 privé.
--
--   version / major_root : version du logiciel attestée et sa racine majeure ;
--     une attestation dont la racine diffère de la version en service est
--     périmée (nouvelle version majeure : nouvelle attestation) ;
--   content : toutes les valeurs reportées dans le document (éditeur,
--     établissement, signataire, dates, périmètre), pour le reproduire ;
--   sha256 : empreinte du PDF, reprise dans l'entrée ATTESTATION_GENERATED du
--     journal d'audit chaîné et signé (date certaine, inaltérabilité).
--
-- ORDRE DE DÉPLOIEMENT : avant le code du lot F.
CREATE TABLE IF NOT EXISTS attestations (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    merchant_id       varchar(64)  NOT NULL,
    software          varchar(64)  NOT NULL,
    version           varchar(32)  NOT NULL,
    major_root        varchar(16)  NOT NULL,
    content           jsonb        NOT NULL,
    signer_name       varchar(255) NOT NULL,
    signed_by_user_id varchar(64)  NOT NULL,
    signed_at         timestamptz  NOT NULL,
    filename          varchar(255) NOT NULL,
    r2_key            varchar(512) NOT NULL,
    sha256            char(64)     NOT NULL,
    size_bytes        bigint       NOT NULL,
    generated_at      timestamptz  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_attestations_merchant
    ON attestations (merchant_id, generated_at DESC, id DESC);

COMMENT ON TABLE attestations IS 'Attestations individuelles de l''éditeur (BOI-LETTRE-000242), une ligne par document généré, jamais modifiée. PDF dans le bucket R2 privé.';
COMMENT ON COLUMN attestations.content IS 'Valeurs reportées dans le document (éditeur, établissement, signataire, dates, périmètre).';
COMMENT ON COLUMN attestations.sha256 IS 'SHA-256 du PDF tel que stocké, repris au journal d''audit chaîné.';
