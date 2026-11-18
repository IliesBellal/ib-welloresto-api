-- Compte client, deuxième lot (docs/customer-accounts.md, « Lot 2 ») :
-- plafond par client, réglage des espèces, relances, liens de paiement en
-- ligne. Après 220_customer_accounts.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Rejouable.

-- 1. Réglages d'un client : plafond d'ardoise propre (NULL = celui de
-- l'établissement) et ardoise interdite (son avance reste utilisable).
CREATE TABLE IF NOT EXISTS customer_account_settings (
  merchant_id    varchar(64) NOT NULL,
  customer_id    integer     NOT NULL,
  credit_limit   integer,
  credit_blocked boolean     NOT NULL DEFAULT false,
  updated_at     timestamptz NOT NULL DEFAULT now(),
  updated_by     varchar(64),
  PRIMARY KEY (merchant_id, customer_id),
  CONSTRAINT customer_account_settings_limit_chk CHECK (credit_limit IS NULL OR credit_limit >= 0)
);
COMMENT ON TABLE customer_account_settings IS 'Compte client : plafond d''ardoise propre au client (NULL = plafond de l''établissement) et ardoise interdite.';

-- 2. Réglages de l'établissement.
--   customer_account_cash_mode : versement en espèces au-delà de 1 000 €
--     BLOCK = refusé ; WARN = accepté jusqu'à 15 000 € si la caisse confirme
--     un client non-résident (code monétaire et financier, D112-3).
--   customer_account_reminder_* : relances automatiques des ardoises.
ALTER TABLE merchant_parameters
  ADD COLUMN IF NOT EXISTS customer_account_cash_mode varchar(10) NOT NULL DEFAULT 'BLOCK',
  ADD COLUMN IF NOT EXISTS customer_account_reminder_enabled boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS customer_account_reminder_days integer NOT NULL DEFAULT 30,
  ADD COLUMN IF NOT EXISTS customer_account_reminder_max integer NOT NULL DEFAULT 3,
  ADD COLUMN IF NOT EXISTS customer_account_reminder_sms boolean NOT NULL DEFAULT false;

-- 3. Relances envoyées (journal, et base du rythme des relances).
CREATE TABLE IF NOT EXISTS customer_account_reminders (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  merchant_id varchar(64) NOT NULL,
  customer_id integer     NOT NULL,
  sent_at     timestamptz NOT NULL DEFAULT now(),
  source      varchar(10) NOT NULL,  -- AUTO (tâche) ou MANUAL (back-office)
  channels    varchar(20) NOT NULL,  -- EMAIL, SMS, EMAIL+SMS
  balance     integer     NOT NULL,  -- solde au moment de la relance (centimes)
  user_id     varchar(64)
);
CREATE INDEX IF NOT EXISTS idx_customer_account_reminders_customer
  ON customer_account_reminders (merchant_id, customer_id, sent_at);

-- 4. Liens de paiement en ligne (établissements avec un compte Stripe) : un
-- jeton public par client ; le montant est la dette au moment du clic.
CREATE TABLE IF NOT EXISTS customer_account_payment_links (
  token       varchar(64) PRIMARY KEY,
  merchant_id varchar(64) NOT NULL,
  customer_id integer     NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  expires_at  timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_customer_account_payment_links_customer
  ON customer_account_payment_links (merchant_id, customer_id);

-- 5. Paiements en ligne reçus (une session Stripe = un versement, une seule
-- fois même si le webhook est rejoué).
CREATE TABLE IF NOT EXISTS customer_account_online_payments (
  checkout_session_id varchar(255) PRIMARY KEY,
  merchant_id         varchar(64)  NOT NULL,
  customer_id         integer      NOT NULL,
  amount              integer      NOT NULL,
  operation_id        bigint       NOT NULL,
  -- Identifiant du paiement chez le prestataire et part déjà remboursée, pour
  -- répercuter automatiquement un remboursement sur le compte du client.
  payment_intent_id   varchar(255),
  refunded_amount     integer      NOT NULL DEFAULT 0,
  created_at          timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_customer_account_online_payments_intent
  ON customer_account_online_payments (payment_intent_id);

-- 6. Libellés : versement et remboursement en ligne.
INSERT INTO labels (label_value, label_type, lang, label)
SELECT v.label_value, 'mop', 'FR', v.label
FROM (VALUES
  ('ACCOUNT_IN_STRIPE',  'Versement compte client · En ligne'),
  ('ACCOUNT_OUT_STRIPE', 'Remboursement compte client · En ligne')
) AS v(label_value, label)
WHERE NOT EXISTS (
  SELECT 1 FROM labels l WHERE l.label_value = v.label_value AND l.label_type = 'mop' AND l.lang = 'FR'
);
