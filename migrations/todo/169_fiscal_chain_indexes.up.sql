-- Conformité caisse, lot A (docs/attestation-conformite-01-lot-A-brief.md,
-- phase 1) : index de recherche du dernier maillon des chaînes fiscales, et
-- unicité du numéro de ticket (constat C5).
--
-- Aujourd'hui, chaque encaissement et chaque clôture cherche le maillon
-- précédent sans index adapté (staging, 2026-10-07, établissement le plus
-- actif) : payments 80 ms (parcours complet), orders 106 ms (tri de 19 000
-- lignes), audit_logs 14 ms, receipts 3 ms. Avec ces index : lecture d'une
-- seule entrée.
--
-- Index partiels « hash IS NOT NULL » sur orders, payments et cash_registers :
-- la recherche du lot A ne considère que les lignes chaînées. Ils sont aussi
-- plus petits (orders : environ 5 000 lignes chaînées sur 34 000 à staging).
-- receipts et audit_logs ont hash NOT NULL : index complets.
--
-- PRÉREQUIS de l'index unique sur receipts : aucun numéro de ticket en double.
-- Staging : 0 (vérifié le 2026-10-07). AVANT d'appliquer en production, lancer :
--
--   SELECT merchant_id, receipt_number, count(*)
--   FROM receipts
--   GROUP BY merchant_id, receipt_number
--   HAVING count(*) > 1;
--
-- Si la requête renvoie des lignes, NE PAS appliquer cette migration : la
-- création de l'index échouerait (et laisserait un index INVALID à supprimer).
-- Ne pas corriger les données : on en discute d'abord.
--
-- ATTENTION - CREATE INDEX CONCURRENTLY doit être joué hors bloc
-- transactionnel, une instruction à la fois, comme les autres CREATE INDEX
-- CONCURRENTLY de ce dépôt (087, 109, 124, 132, 163). Aucun verrou bloquant
-- sur les tables pendant la construction.
--
-- ORDRE DE DÉPLOIEMENT : appliquer AVANT de déployer le code du lot A. Le code
-- actuel en profite déjà en partie (payments, receipts, audit_logs), sans en
-- dépendre.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_payments_fiscal_chain_head
    ON payments (merchant_id, payment_date DESC, payment_id DESC)
    WHERE hash IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_fiscal_chain_head
    ON orders (merchant_id, delivered_on DESC, order_id DESC)
    WHERE hash IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_receipts_fiscal_chain_head
    ON receipts (merchant_id, created_at DESC, receipt_number DESC);

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_receipts_merchant_receipt_number
    ON receipts (merchant_id, receipt_number);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_audit_logs_fiscal_chain_head
    ON audit_logs (merchant_id, created_at DESC, id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_cash_registers_fiscal_chain_head
    ON cash_registers (merchant_id, end_date DESC)
    WHERE hash IS NOT NULL;
