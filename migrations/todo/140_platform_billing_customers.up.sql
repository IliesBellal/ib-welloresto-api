-- LOT B B2a-1 (docs/decisions.md) : platform_billing_customers — le
-- Customer Stripe que WelloResto utilise pour FACTURER le marchand
-- (abonnement plateforme). Nom choisi sans ambiguïté avec
-- welloresto_stripe_customers (confirmé lié au compte Stripe CONNECTÉ —
-- paiements des clients finaux du marchand — pas à cette relation).
--
-- merchant_id est text (comme partout ailleurs dans ce dépôt) alors que
-- merchant.id est integer : une contrainte FK littérale
-- "REFERENCES merchant(id)" échoue à la création (types incompatibles,
-- vérifié directement contre staging — SQLSTATE 42804), exactement pour la
-- raison déjà documentée sur subscription_items/subscription_overrides. Pas
-- de FK ici non plus, une UNIQUE suffit à l'invariant "un merchant_id, une
-- ligne, un stripe_customer_id".
CREATE TABLE platform_billing_customers (
    id                      text PRIMARY KEY,
    merchant_id             text NOT NULL,
    stripe_customer_id      text NOT NULL,
    is_primary_for_merchant boolean NOT NULL DEFAULT true,
    created_at              timestamptz NOT NULL DEFAULT now()
);
COMMENT ON COLUMN platform_billing_customers.is_primary_for_merchant IS 'true = ce Customer Stripe appartient en propre à ce merchant_id. false = mutualisation : ce merchant_id partage le Customer d''un autre (voir POST .../billing-customer/attach-to/{other_merchant_id}).';

CREATE UNIQUE INDEX idx_platform_billing_customers_merchant_id ON platform_billing_customers (merchant_id);

-- Plusieurs merchant_id peuvent partager le même stripe_customer_id
-- (mutualisation) — jamais l'inverse (garanti par l'UNIQUE ci-dessus sur
-- merchant_id). Cet index sert à retrouver tous les merchant_id d'un
-- Customer partagé (ex. avant un détachement).
CREATE INDEX idx_platform_billing_customers_stripe_customer_id ON platform_billing_customers (stripe_customer_id);
