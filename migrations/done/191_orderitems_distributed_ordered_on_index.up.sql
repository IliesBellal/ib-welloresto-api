-- Temps de préparation moyen (internal/tasks/distribution.go,
-- computeDistributionStats) : toutes les 15 min, une agrégation lit les
-- orderitems servis des dernières 24h, tous marchands confondus. Sans index
-- sur ordered_on, chaque passage parcourt toute la table, qui grossit avec
-- l'historique. L'index partiel ne couvre que les lignes servies (le prédicat
-- reprend exactement le WHERE de la requête) : il reste petit.
--
-- ATTENTION - CREATE INDEX CONCURRENTLY doit être joué hors bloc
-- transactionnel, comme les autres index CONCURRENTLY de ce dépôt.
--
-- ORDRE DE DÉPLOIEMENT : indifférent (le code fonctionne sans, plus lentement).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orderitems_distributed_ordered_on
    ON orderitems (ordered_on)
    WHERE distributed_quantity > 0 AND distributed_on IS NOT NULL;
