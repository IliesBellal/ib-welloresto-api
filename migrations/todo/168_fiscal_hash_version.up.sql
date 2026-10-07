-- Conformité caisse, lot A (docs/attestation-conformite-01-lot-A-brief.md,
-- phase 1) : version du schéma d'empreinte sur les cinq chaînes fiscales, et
-- signature à clé du journal d'audit.
--
--   hash_version : formule qui a produit hash/signature de la ligne.
--     1 = formule historique (DEFAULT : toutes les lignes existantes, et toute
--         ligne qu'un chemin non migré écrirait encore) ;
--     2 = empreinte complète du lot A (constat C3), écrite explicitement par le
--         code. Une ligne sans hash garde 1 : la colonne ne dit rien de plus.
--   audit_logs.signature : HMAC de hash (security.SignHash), comme les quatre
--     autres chaînes. NULL pour les lignes antérieures.
--
-- Aucune réécriture de table : un ADD COLUMN avec DEFAULT constant est
-- instantané en Postgres 11+ (staging : 18.4). Chaque ALTER prend toutefois un
-- verrou exclusif bref : lock_timeout évite qu'un ALTER bloqué derrière une
-- longue requête fasse attendre tout le trafic derrière lui. En cas d'échec
-- sur ce délai, relancer simplement (IF NOT EXISTS).
--
-- ORDRE DE DÉPLOIEMENT : appliquer AVANT de déployer le code du lot A, qui
-- écrit hash_version = 2. Le code actuel ignore ces colonnes.
SET lock_timeout = '5s';

ALTER TABLE receipts       ADD COLUMN IF NOT EXISTS hash_version smallint NOT NULL DEFAULT 1;
ALTER TABLE orders         ADD COLUMN IF NOT EXISTS hash_version smallint NOT NULL DEFAULT 1;
ALTER TABLE payments       ADD COLUMN IF NOT EXISTS hash_version smallint NOT NULL DEFAULT 1;
ALTER TABLE cash_registers ADD COLUMN IF NOT EXISTS hash_version smallint NOT NULL DEFAULT 1;
ALTER TABLE audit_logs     ADD COLUMN IF NOT EXISTS hash_version smallint NOT NULL DEFAULT 1;
ALTER TABLE audit_logs     ADD COLUMN IF NOT EXISTS signature text;

COMMENT ON COLUMN receipts.hash_version IS 'Formule d''empreinte : 1 = historique, 2 = complète (lot A conformité caisse).';
COMMENT ON COLUMN orders.hash_version IS 'Formule d''empreinte de clôture : 1 = historique, 2 = complète (lot A conformité caisse).';
COMMENT ON COLUMN payments.hash_version IS 'Formule d''empreinte : 1 = historique, 2 = complète (lot A conformité caisse).';
COMMENT ON COLUMN cash_registers.hash_version IS 'Formule d''empreinte de fermeture : 1 = historique, 2 = complète (lot A conformité caisse).';
COMMENT ON COLUMN audit_logs.hash_version IS 'Formule d''empreinte : 1 = historique, 2 = complète et signée (lot A conformité caisse).';
COMMENT ON COLUMN audit_logs.signature IS 'HMAC de hash (FISCAL_SIGNING_KEY). NULL avant le lot A conformité caisse.';

RESET lock_timeout;
