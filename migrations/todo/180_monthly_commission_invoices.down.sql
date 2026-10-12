-- Reverts 180_monthly_commission_invoices.up.sql : retour à une facture par
-- virement (table de la migration 178). Les factures mensuelles émises sont
-- perdues : à ne jouer que sur un environnement de test.
DROP TABLE IF EXISTS commission_invoices;

CREATE TABLE commission_invoices (
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

CREATE INDEX idx_commission_invoices_merchant ON commission_invoices (merchant_id, issued_at);

DROP INDEX IF EXISTS idx_payout_documents_to_invoice;
ALTER TABLE payout_documents
    DROP COLUMN IF EXISTS scannorder_amount,
    DROP COLUMN IF EXISTS kiosk_amount,
    DROP COLUMN IF EXISTS other_amount,
    DROP COLUMN IF EXISTS refunds_amount,
    DROP COLUMN IF EXISTS adjustments_amount,
    DROP COLUMN IF EXISTS commission_ttc,
    DROP COLUMN IF EXISTS stripe_fees,
    DROP COLUMN IF EXISTS discrepancy,
    DROP COLUMN IF EXISTS invoicing_state,
    DROP COLUMN IF EXISTS commission_invoice_id;
