-- Conformité caisse, lot D (docs/attestation-conformite-05-lot-D-brief.md) :
-- archives fiscales (BOI-TVA-DECLA-30-10-30 §220 à §250). Une ligne par
-- archive générée — automatiquement pour chaque mois clos, ou à la demande pour
-- une période close —, jamais modifiée ni supprimée. Le fichier (ZIP : CSV,
-- notice en français, manifeste) est dans le bucket R2 privé.
--
-- Chaque ligne est chaînée et signée comme les autres chaînes fiscales
-- (internal/fiscal, chaîne fiscal_archives, hash_version 2) : l'empreinte
-- couvre l'établissement, la période, le SHA-256 du fichier et de son
-- manifeste, la version du logiciel et la date de génération. C'est ce
-- chaînage signé qui fige l'archive et lui donne date certaine (§220) ; la
-- table elle-même est le journal de génération (§240).
--
--   kind : MONTH (automatique, une par mois) ou PERIOD (à la demande) ;
--   period_start / period_end : jours locaux de l'établissement, inclus ;
--   generated_by : SYSTEM (tâche) ou l'utilisateur qui l'a demandée.
--
-- ORDRE DE DÉPLOIEMENT : avant le code du lot D (la tâche écrit dans cette
-- table).
CREATE TABLE IF NOT EXISTS fiscal_archives (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    merchant_id      varchar(64)  NOT NULL,
    kind             varchar(10)  NOT NULL CHECK (kind IN ('MONTH', 'PERIOD')),
    period_start     date         NOT NULL,
    period_end       date         NOT NULL,
    timezone         varchar(50)  NOT NULL,
    filename         varchar(255) NOT NULL,
    r2_key           varchar(512) NOT NULL,
    sha256           char(64)     NOT NULL,
    manifest_sha256  char(64)     NOT NULL,
    size_bytes       bigint       NOT NULL,
    software_version varchar(32)  NOT NULL,
    generated_by     varchar(64)  NOT NULL,
    generated_at     timestamptz  NOT NULL,
    previous_hash    varchar(64)  NOT NULL,
    hash             varchar(64)  NOT NULL,
    signature        text         NOT NULL,
    hash_version     smallint     NOT NULL DEFAULT 2,
    CONSTRAINT chk_fiscal_archives_period CHECK (period_end >= period_start)
);

-- Une seule archive automatique par établissement et par mois.
CREATE UNIQUE INDEX IF NOT EXISTS uq_fiscal_archives_month
    ON fiscal_archives (merchant_id, period_start)
    WHERE kind = 'MONTH';

-- Tête de chaîne (dernier maillon de l'établissement) et liste du back-office.
CREATE INDEX IF NOT EXISTS idx_fiscal_archives_chain_head
    ON fiscal_archives (merchant_id, generated_at DESC, id DESC);

COMMENT ON TABLE fiscal_archives IS 'Archives fiscales (BOI §220-§250) : une ligne par archive générée, chaînée et signée, jamais modifiée. Fichier dans le bucket R2 privé.';
COMMENT ON COLUMN fiscal_archives.sha256 IS 'SHA-256 du fichier ZIP tel que stocké.';
COMMENT ON COLUMN fiscal_archives.manifest_sha256 IS 'SHA-256 du MANIFEST.json contenu dans l''archive (empreinte de chaque fichier).';
