-- Conformité caisse, lot B (docs/attestation-conformite-02-lot-B-brief.md) :
-- clôtures fiscales journalières, mensuelles et annuelles (BOI-TVA-DECLA-30-10-30
-- §170), produites par le logiciel sans action du restaurateur. La clôture
-- journalière scelle aussi chaque commande clôturée ce jour-là (décision S1 :
-- remplace l'empreinte portée par la ligne orders).
--
-- Table en ajout seul, une ligne par établissement, type de période et
-- période ; chaînée et signée comme les autres chaînes fiscales
-- (internal/fiscal, hash_version 2). Montants en centimes.
--
--   period_start / period_end : jours locaux de l'établissement (timezone),
--     bornes incluses (DAY : un seul jour).
--   sales_* : tickets de vente ; refunds_* : avoirs (négatifs) ; net_* : somme.
--   vat_by_rate, payments_by_mop, by_channel : ventilations (jsonb).
--   orders (DAY) : [{order_id, kind, hash}] des commandes closes ce jour-là.
--   opening : première clôture de l'établissement seulement — valeur
--     d'ouverture du total perpétuel et du grand total (somme des tickets
--     antérieurs), avec méthode et date de reprise.
--   grand_total_period : cumul du net TTC depuis le 1er janvier ;
--   perpetual_total : cumul depuis le début (jamais remis à zéro).
--
-- ORDRE DE DÉPLOIEMENT : appliquer AVANT le code du lot B (la tâche de nuit
-- écrit dans cette table).
CREATE TABLE IF NOT EXISTS fiscal_closures (
    id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    merchant_id          varchar(64) NOT NULL,
    period_type          varchar(5)  NOT NULL CHECK (period_type IN ('DAY', 'MONTH', 'YEAR')),
    period_start         date        NOT NULL,
    period_end           date        NOT NULL,
    timezone             varchar(50) NOT NULL,
    closed_at            timestamptz NOT NULL,
    sales_ttc            bigint      NOT NULL,
    sales_ht             bigint      NOT NULL,
    refunds_ttc          bigint      NOT NULL,
    refunds_ht           bigint      NOT NULL,
    net_ttc              bigint      NOT NULL,
    net_ht               bigint      NOT NULL,
    vat_by_rate          jsonb       NOT NULL,
    payments_by_mop      jsonb       NOT NULL,
    by_channel           jsonb       NOT NULL,
    receipts_count       integer     NOT NULL,
    first_receipt_number varchar(50),
    last_receipt_number  varchar(50),
    orders_count         integer     NOT NULL,
    orders               jsonb,
    opening              jsonb,
    grand_total_period   bigint      NOT NULL,
    perpetual_total      bigint      NOT NULL,
    previous_hash        varchar(64) NOT NULL,
    hash                 varchar(64) NOT NULL,
    signature            text        NOT NULL,
    hash_version         smallint    NOT NULL DEFAULT 2,
    CONSTRAINT uq_fiscal_closures_period UNIQUE (merchant_id, period_type, period_start),
    CONSTRAINT chk_fiscal_closures_period CHECK (period_end >= period_start)
);

-- Tête de chaîne (dernier maillon de l'établissement).
CREATE INDEX IF NOT EXISTS idx_fiscal_closures_chain_head
    ON fiscal_closures (merchant_id, closed_at DESC, id DESC);

COMMENT ON TABLE fiscal_closures IS 'Clôtures fiscales journalières, mensuelles et annuelles (BOI-TVA-DECLA-30-10-30 §170), scellées et chaînées. Ajout seul.';
COMMENT ON COLUMN fiscal_closures.orders IS 'Clôture DAY : empreinte de chaque commande close ce jour-là [{order_id, kind, hash}] (scellement des commandes, décision S1).';
COMMENT ON COLUMN fiscal_closures.opening IS 'Première clôture seulement : valeur d''ouverture du total perpétuel et du grand total (somme des tickets antérieurs), méthode et date de reprise.';
COMMENT ON COLUMN fiscal_closures.grand_total_period IS 'Cumul du net TTC (centimes) depuis le 1er janvier, cette période incluse.';
COMMENT ON COLUMN fiscal_closures.perpetual_total IS 'Cumul du net TTC (centimes) depuis le début, jamais remis à zéro.';
