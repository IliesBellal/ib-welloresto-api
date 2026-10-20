-- Reverts 168_fiscal_hash_version.up.sql. À n'appliquer qu'après avoir retiré
-- du code déployé l'écriture de hash_version et de audit_logs.signature :
-- supprimer ces colonnes efface l'information qui permet de vérifier les
-- lignes écrites en version 2.
SET lock_timeout = '5s';

ALTER TABLE audit_logs     DROP COLUMN IF EXISTS signature;
ALTER TABLE audit_logs     DROP COLUMN IF EXISTS hash_version;
ALTER TABLE cash_registers DROP COLUMN IF EXISTS hash_version;
ALTER TABLE payments       DROP COLUMN IF EXISTS hash_version;
ALTER TABLE orders         DROP COLUMN IF EXISTS hash_version;
ALTER TABLE receipts       DROP COLUMN IF EXISTS hash_version;

RESET lock_timeout;
