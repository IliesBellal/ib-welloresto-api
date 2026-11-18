-- Retour arrière de 220_customer_accounts. À ne jouer qu'avant tout usage en
-- production : les opérations scellées et les colonnes de paiement sont des
-- données fiscales.
DELETE FROM labels
WHERE label_type = 'mop' AND lang = 'FR'
  AND label_value IN ('ACCOUNT', 'ACCOUNT_IN_ES', 'ACCOUNT_IN_CB', 'ACCOUNT_IN_CHEQUE', 'ACCOUNT_IN_TRANSFER',
                      'ACCOUNT_OUT_ES', 'ACCOUNT_OUT_CB', 'ACCOUNT_OUT_CHEQUE', 'ACCOUNT_OUT_TRANSFER');
-- CHEQUE et TRANSFER sont laissés : ils ont pu servir ailleurs.

ALTER TABLE fiscal_closures DROP COLUMN IF EXISTS account_operations;
ALTER TABLE merchant_parameters DROP COLUMN IF EXISTS customer_account_credit_limit;
DROP INDEX IF EXISTS idx_payments_account_customer;
ALTER TABLE payments DROP COLUMN IF EXISTS customer_id;
DROP TABLE IF EXISTS customer_account_operations;
