-- Factures d'abonnement envoyées aux restaurateurs (docs/payouts-justificatifs.md,
-- « Factures d'abonnement ») : pour chaque paiement d'abonnement Stripe Billing
-- (événement invoice.paid), le PDF de la facture Stripe est envoyé par mail et
-- archivé dans le bucket R2 privé, puis listé dans le back-office.
--
-- File de travail + journal, une ligne par facture Stripe (stripe_invoice_id
-- unique : le webhook peut être rejoué sans doublon). La tâche horaire
-- RunSubscriptionInvoiceDocuments traite les lignes 'pending' ; 'failed' =
-- abandon après trop de tentatives.
--
-- ORDRE DE DÉPLOIEMENT : avant le code (le webhook invoice.paid écrit dans
-- cette table).
CREATE TABLE IF NOT EXISTS subscription_invoice_documents (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    stripe_invoice_id varchar(64)  NOT NULL,
    merchant_id       varchar(64)  NOT NULL,
    number            varchar(64),
    amount_paid       bigint       NOT NULL,
    currency          varchar(8)   NOT NULL DEFAULT 'eur',
    period_start      timestamptz,
    period_end        timestamptz,
    paid_at           timestamptz,
    status            varchar(16)  NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'done', 'failed')),
    attempts          integer      NOT NULL DEFAULT 0,
    last_error        text,
    recipient         varchar(255),
    pdf_key           varchar(512),
    created_at        timestamptz  NOT NULL DEFAULT now(),
    sent_at           timestamptz,
    CONSTRAINT uq_subscription_invoice_documents_invoice UNIQUE (stripe_invoice_id)
);

CREATE INDEX IF NOT EXISTS idx_subscription_invoice_documents_pending ON subscription_invoice_documents (id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_subscription_invoice_documents_merchant ON subscription_invoice_documents (merchant_id, paid_at);
