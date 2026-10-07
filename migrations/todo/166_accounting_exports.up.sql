-- Archivage des exports comptables (docs/EXPORT_COMPTABLE_MODES_CLOTURE.md).
--
-- Jusqu'ici, chaque export était déposé en lecture publique sur R2 sous un nom
-- fixe (WR_rapport_comptable_AAAA_MM.pdf) : un export régénéré écrasait celui
-- envoyé au comptable, et l'adresse se devinait (identifiant d'établissement
-- séquentiel). Désormais chaque export est déposé dans le bucket PRIVÉ sous un
-- nom unique horodaté, et référencé ici : on peut le retrouver, le renvoyer
-- (lien signé à durée limitée) et prouver qu'il n'a pas été modifié (sha256).
-- Les fichiers déjà publiés ne sont ni supprimés ni déplacés.
--
-- ORDRE DE DÉPLOIEMENT : appliquer AVANT de déployer le code (l'export écrit
-- dans cette table). Rejouable : IF NOT EXISTS.

CREATE TABLE IF NOT EXISTS accounting_exports (
    id           bigserial    PRIMARY KEY,
    merchant_id  varchar(64)  NOT NULL,
    period_from  date         NOT NULL,
    period_to    date         NOT NULL,
    closing_mode varchar(10)  NOT NULL,
    channels     varchar(200) NOT NULL DEFAULT '',
    filename     varchar(255) NOT NULL,
    r2_key       varchar(512) NOT NULL,
    sha256       char(64)     NOT NULL,
    size_bytes   integer      NOT NULL,
    generated_by varchar(64)  NOT NULL,
    generated_at timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT chk_accounting_exports_closing_mode CHECK (closing_mode IN ('MANUAL', 'AUTO'))
);

CREATE INDEX IF NOT EXISTS idx_accounting_exports_merchant
    ON accounting_exports (merchant_id, generated_at DESC);

COMMENT ON TABLE accounting_exports IS 'Exports comptables PDF générés (un par génération, jamais écrasés) : fichier dans le bucket R2 privé, téléchargé par lien signé.';
COMMENT ON COLUMN accounting_exports.period_from IS 'Premier jour de la période (calendrier de l''établissement).';
COMMENT ON COLUMN accounting_exports.period_to IS 'Dernier jour inclus de la période (calendrier de l''établissement).';
COMMENT ON COLUMN accounting_exports.channels IS 'Canaux retenus (orders.order_source, séparés par des virgules) ; vide = tous les canaux.';
COMMENT ON COLUMN accounting_exports.r2_key IS 'Clé de l''objet dans le bucket R2 privé (R2_PRIVATE_BUCKET).';
COMMENT ON COLUMN accounting_exports.sha256 IS 'Empreinte SHA-256 du PDF tel que généré.';
