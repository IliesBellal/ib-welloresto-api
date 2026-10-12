-- Facture de commission MENSUELLE (docs/payouts-justificatifs.md) : une facture
-- par établissement et par mois, indépendante du rythme des virements. Elle
-- additionne la commission de tous les virements non encore facturés arrivés
-- avant le début du mois courant ; un virement traité en retard est donc repris
-- le mois suivant, sans trou ni doublon.
--
--   payout_documents : chaque virement garde ses totaux (de quoi construire le
--                      récapitulatif du mois sans relire Stripe) et son état de
--                      facturation : 'pending' (à facturer), 'invoiced'
--                      (rattaché à commission_invoice_id) ou 'skipped'
--                      (commission nulle ou négative, ou virement de test
--                      antérieur à cette migration, sans totaux).
--   commission_invoices : refaite à zéro (seules des factures de la série
--                      d'essai TEST- existaient) ; une ligne par (établissement,
--                      mois). 'pending' = émise mais pas encore envoyée (mail à
--                      retenter), 'sent', 'failed' = abandon.
--
-- ORDRE DE DÉPLOIEMENT : avant le code.
ALTER TABLE payout_documents
    ADD COLUMN IF NOT EXISTS scannorder_amount     bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS kiosk_amount          bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS other_amount          bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS refunds_amount        bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS adjustments_amount    bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS commission_ttc        bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS stripe_fees           bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS discrepancy           bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS invoicing_state       varchar(16) NOT NULL DEFAULT 'pending'
        CHECK (invoicing_state IN ('pending', 'invoiced', 'skipped')),
    ADD COLUMN IF NOT EXISTS commission_invoice_id bigint;

-- Virements déjà traités avant cette migration (essais) : sans totaux, ils ne
-- doivent pas entrer dans une facture mensuelle.
UPDATE payout_documents SET invoicing_state = 'skipped' WHERE status = 'done';

CREATE INDEX IF NOT EXISTS idx_payout_documents_to_invoice
    ON payout_documents (merchant_id, arrival_date) WHERE status = 'done' AND invoicing_state = 'pending';

DROP TABLE IF EXISTS commission_invoices;
DELETE FROM invoice_counters WHERE series = 'TEST';

CREATE TABLE commission_invoices (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    number       varchar(32)  NOT NULL,
    series       varchar(16)  NOT NULL,
    merchant_id  varchar(64)  NOT NULL,
    period_month date         NOT NULL,
    amount_ttc   bigint       NOT NULL,
    amount_ht    bigint       NOT NULL,
    amount_vat   bigint       NOT NULL,
    payout_count integer      NOT NULL,
    period_start timestamptz  NOT NULL,
    period_end   timestamptz  NOT NULL,
    issued_at    timestamptz  NOT NULL DEFAULT now(),
    status       varchar(16)  NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts     integer      NOT NULL DEFAULT 0,
    last_error   text,
    recipient    varchar(255),
    invoice_key  varchar(512),
    recap_key    varchar(512),
    sent_at      timestamptz,
    CONSTRAINT uq_commission_invoices_number UNIQUE (number),
    CONSTRAINT uq_commission_invoices_month UNIQUE (merchant_id, period_month)
);

CREATE INDEX idx_commission_invoices_unsent ON commission_invoices (id) WHERE status = 'pending';
CREATE INDEX idx_commission_invoices_merchant ON commission_invoices (merchant_id, period_month);
