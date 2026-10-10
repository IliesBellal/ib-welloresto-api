-- Justificatifs de versement (docs/payouts-justificatifs.md) : pour chaque payout
-- Stripe d'un restaurateur, un relevé de versement et la facture de la
-- commission Wello Resto, envoyés par mail.
--
--   payout_documents    : file de travail + journal, une ligne par payout
--                         (payout_id unique : le webhook payout.paid peut être
--                         rejoué sans créer de doublon). La tâche horaire
--                         RunPayoutDocuments traite les lignes 'pending' ;
--                         'failed' = abandon après trop de tentatives.
--   commission_invoices : factures de commission émises, une par payout,
--                         immuables ; number est unique et sans trou.
--   invoice_counters    : dernier numéro attribué par (série, année). La série
--                         TEST- sert à la phase d'essai, WR- à la numérotation
--                         légale ; une ligne est incrémentée dans la même
--                         transaction que l'insertion de la facture, donc un
--                         échec ne laisse jamais de trou.
--
-- ORDRE DE DÉPLOIEMENT : avant le code (le webhook payout.paid écrit dans
-- payout_documents).
CREATE TABLE IF NOT EXISTS payout_documents (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    payout_id         varchar(64)  NOT NULL,
    stripe_account_id varchar(64)  NOT NULL,
    merchant_id       varchar(64),
    amount            bigint       NOT NULL,
    currency          varchar(8)   NOT NULL DEFAULT 'eur',
    arrival_date      timestamptz  NOT NULL,
    status            varchar(16)  NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'done', 'failed')),
    attempts          integer      NOT NULL DEFAULT 0,
    last_error        text,
    recipient         varchar(255),
    statement_key     varchar(512),
    invoice_key       varchar(512),
    created_at        timestamptz  NOT NULL DEFAULT now(),
    sent_at           timestamptz,
    CONSTRAINT uq_payout_documents_payout UNIQUE (payout_id)
);

CREATE INDEX IF NOT EXISTS idx_payout_documents_pending ON payout_documents (id) WHERE status = 'pending';

CREATE TABLE IF NOT EXISTS invoice_counters (
    series      varchar(16) NOT NULL,
    year        integer     NOT NULL,
    last_number integer     NOT NULL DEFAULT 0,
    PRIMARY KEY (series, year)
);

CREATE TABLE IF NOT EXISTS commission_invoices (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    number       varchar(32)  NOT NULL,
    series       varchar(16)  NOT NULL,
    merchant_id  varchar(64)  NOT NULL,
    payout_id    varchar(64)  NOT NULL,
    amount_ttc   bigint       NOT NULL,
    amount_ht    bigint       NOT NULL,
    amount_vat   bigint       NOT NULL,
    period_start timestamptz  NOT NULL,
    period_end   timestamptz  NOT NULL,
    issued_at    timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT uq_commission_invoices_number UNIQUE (number),
    CONSTRAINT uq_commission_invoices_payout UNIQUE (payout_id)
);

CREATE INDEX IF NOT EXISTS idx_commission_invoices_merchant ON commission_invoices (merchant_id, issued_at);
