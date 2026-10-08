-- Conformité caisse, lots B et D (docs/attestation-conformite-05-lot-D-brief.md,
-- phase 2) : la clôture journalière (une fois par jour et par établissement)
-- et l'archive mensuelle lisent les paiements et les commandes closes d'un
-- établissement sur une période. Sans ces index :
--   - paiements : parcours complet de la table (le seul index
--     établissement + date, idx_payments_fiscal_chain_head, est partiel :
--     WHERE hash IS NOT NULL, inutilisable ici) ;
--   - commandes closes : toutes les commandes de l'établissement, filtrées
--     ensuite sur delivered_on.
-- Coût : un index de plus à l'insertion d'un paiement et à la clôture d'une
-- commande (quelques microsecondes) ; aucun effet sur les lectures de la
-- caisse.
--
-- ATTENTION - CREATE INDEX CONCURRENTLY doit être joué hors bloc
-- transactionnel, un index par commande, comme les autres index CONCURRENTLY
-- de ce dépôt.
--
-- ORDRE DE DÉPLOIEMENT : indifférent (le code fonctionne avec ou sans) ;
-- avant le rattrapage des clôtures et des archives, qui en profite le plus.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_payments_merchant_date
    ON payments (merchant_id, payment_date);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_merchant_closed
    ON orders (merchant_id, delivered_on)
    WHERE state = 'CLOSED';
