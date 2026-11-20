-- Compte client : avance et ardoise (docs/customer-accounts.md).
--
-- Un seul solde par client et par établissement, positif (avance versée) ou
-- négatif (ardoise). Il n'est stocké nulle part : il se calcule à partir de
-- deux sources, toutes deux scellées dans une chaîne fiscale :
--
--   + customer_account_operations : argent versé ou rendu hors de toute vente
--     (versement, remboursement du solde) et abandons de créance ;
--   - payments (mop = 'ACCOUNT', actifs) : commandes réglées « sur le compte »
--     (un remboursement de commande sur le compte est un paiement négatif).
--
-- Un versement n'est pas une vente : ni ticket, ni TVA, ni chiffre
-- d'affaires (crédit utilisable sur tous les produits = bon à usages
-- multiples, CGI art. 256 ter : TVA due à la consommation). La vente, et sa
-- TVA, est la commande payée par le compte.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code (le code lit ces colonnes et
-- cette table). Rejouable.

-- 1. Mouvements du compte, chaîne fiscale « customer_account_operations ».
CREATE TABLE IF NOT EXISTS customer_account_operations (
  operation_id     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  merchant_id      varchar(64)  NOT NULL,
  customer_id      integer      NOT NULL,
  operation_type   varchar(20)  NOT NULL,
  amount           integer      NOT NULL,
  mop              varchar(20),
  comment          varchar(250),
  user_id          varchar(64)  NOT NULL,
  cash_register_id varchar(20),
  operation_date   timestamptz  NOT NULL DEFAULT now(),
  previous_hash    varchar(64),
  hash             varchar(64),
  signature        text,
  hash_version     integer,
  CONSTRAINT customer_account_operations_type_chk
    CHECK (operation_type IN ('DEPOSIT', 'REFUND', 'WRITE_OFF')),
  -- amount = effet sur le solde, en centimes : un versement et un abandon de
  -- créance le montent, un remboursement le baisse.
  CONSTRAINT customer_account_operations_sign_chk
    CHECK ((operation_type = 'REFUND' AND amount < 0) OR (operation_type <> 'REFUND' AND amount > 0)),
  -- L'argent passe par un moyen de paiement ; un abandon de créance, non.
  CONSTRAINT customer_account_operations_mop_chk
    CHECK ((operation_type = 'WRITE_OFF') = (mop IS NULL))
);

COMMENT ON TABLE customer_account_operations IS 'Compte client : versements, remboursements du solde et abandons de créance (hors ventes). Scellé et chaîné par établissement (chaîne customer_account_operations). Jamais modifié ni supprimé : une erreur se corrige par une opération inverse.';
COMMENT ON COLUMN customer_account_operations.amount IS 'Effet sur le solde du client, en centimes : DEPOSIT > 0, REFUND < 0, WRITE_OFF > 0.';
COMMENT ON COLUMN customer_account_operations.mop IS 'Moyen de paiement de l''argent versé ou rendu (ES, CB, CHEQUE, TRANSFER). NULL pour un abandon de créance.';
COMMENT ON COLUMN customer_account_operations.cash_register_id IS 'Registre de caisse sur lequel l''argent est passé (Z). NULL pour un abandon de créance.';

CREATE INDEX IF NOT EXISTS idx_customer_account_operations_customer
  ON customer_account_operations (merchant_id, customer_id, operation_date);
CREATE INDEX IF NOT EXISTS idx_customer_account_operations_date
  ON customer_account_operations (merchant_id, operation_date);
CREATE INDEX IF NOT EXISTS idx_customer_account_operations_register
  ON customer_account_operations (cash_register_id) WHERE cash_register_id IS NOT NULL;

-- 2. Client débité par un paiement « sur le compte » (scellé avec le
-- paiement). NULL pour tout autre moyen de paiement.
ALTER TABLE payments ADD COLUMN IF NOT EXISTS customer_id integer;
COMMENT ON COLUMN payments.customer_id IS 'Paiement sur le compte client (mop = ACCOUNT) : client débité ou recrédité. NULL sinon.';
CREATE INDEX IF NOT EXISTS idx_payments_account_customer
  ON payments (merchant_id, customer_id) WHERE mop = 'ACCOUNT';

-- 3. Plafond d'ardoise de l'établissement (centimes). 0 = ardoise désactivée :
-- seul un solde versé d'avance peut alors être consommé.
ALTER TABLE merchant_parameters
  ADD COLUMN IF NOT EXISTS customer_account_credit_limit integer NOT NULL DEFAULT 0;
COMMENT ON COLUMN merchant_parameters.customer_account_credit_limit IS 'Ardoise : dette maximale d''un client, en centimes. 0 = ardoise désactivée.';

-- 4. Clôtures fiscales : mouvements de compte du jour (NULL avant ce lot et
-- les jours sans mouvement, ce qui garde l'empreinte des clôtures existantes).
ALTER TABLE fiscal_closures ADD COLUMN IF NOT EXISTS account_operations jsonb;

-- 5. Libellés des nouveaux moyens et des lignes de compte du Z.
INSERT INTO labels (label_value, label_type, lang, label)
SELECT v.value, 'mop', 'FR', v.label
FROM (VALUES
  ('ACCOUNT',              'Compte client'),
  ('CHEQUE',               'Chèque'),
  ('TRANSFER',             'Virement'),
  ('ACCOUNT_IN_ES',        'Versement compte client · Espèces'),
  ('ACCOUNT_IN_CB',        'Versement compte client · CB'),
  ('ACCOUNT_IN_CHEQUE',    'Versement compte client · Chèque'),
  ('ACCOUNT_IN_TRANSFER',  'Versement compte client · Virement'),
  ('ACCOUNT_OUT_ES',       'Remboursement compte client · Espèces'),
  ('ACCOUNT_OUT_CB',       'Remboursement compte client · CB'),
  ('ACCOUNT_OUT_CHEQUE',   'Remboursement compte client · Chèque'),
  ('ACCOUNT_OUT_TRANSFER', 'Remboursement compte client · Virement')
) AS v(value, label)
WHERE NOT EXISTS (
  SELECT 1 FROM labels l
  WHERE l.label_value = v.value AND l.label_type = 'mop' AND l.lang = 'FR'
);
