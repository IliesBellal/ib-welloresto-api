-- Conformité caisse, lot C (docs/attestation-conformite-03-lot-C-brief.md, R2) :
-- chaque clôture de commande lit désormais le dernier ticket de vente de la
-- commande (reclôture d'une commande rouverte : rien, ou avoir puis nouveau
-- ticket), et chaque annulation regarde si la commande a un ticket. Aucun
-- index ne couvrait receipts.order_id : sans lui, ces lectures parcourent
-- toute la table. Les lectures existantes du ticket d'une commande (facture,
-- remboursement) en profitent aussi.
--
-- ATTENTION - CREATE INDEX CONCURRENTLY doit être joué hors bloc
-- transactionnel, comme les autres index CONCURRENTLY de ce dépôt.
--
-- ORDRE DE DÉPLOIEMENT : avant le code du lot C.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_receipts_order_id
    ON receipts (order_id, created_at DESC);
