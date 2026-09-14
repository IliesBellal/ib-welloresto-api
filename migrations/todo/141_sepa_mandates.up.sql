-- LOT B B2a-3 (docs/decisions.md) : sepa_mandates — trace de l'acceptation
-- d'un mandat SEPA (setup_intent.succeeded), déjà spécifiée §7.4/§11.5.4.
-- merchant_id en text, sans FK, même raison que 140_platform_billing_customers.
CREATE TABLE sepa_mandates (
    id                       text PRIMARY KEY,
    merchant_id              text NOT NULL,
    stripe_payment_method_id text NOT NULL,
    status                   text NOT NULL,
    last4_iban_masked        text,
    accepted_at              timestamptz,
    created_at               timestamptz NOT NULL DEFAULT now()
);
COMMENT ON COLUMN sepa_mandates.status IS 'Statut du mandat côté WelloResto (ex. "active") — pas contraint en base, voir billing.ValidMandateStatuses côté Go.';
COMMENT ON COLUMN sepa_mandates.last4_iban_masked IS '4 derniers chiffres de l''IBAN (PaymentMethod.sepa_debit.last4 côté Stripe) — jamais l''IBAN complet.';

CREATE INDEX idx_sepa_mandates_merchant_id ON sepa_mandates (merchant_id);
