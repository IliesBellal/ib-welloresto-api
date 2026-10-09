-- Fidélité : customer_loyalty_progress et customer_loyalty_progress_order
-- n'avaient aucune contrainte d'unicité — UpdateLoyaltyFromOrder
-- (internal/modules/customers/repository.go) protège contre un double
-- traitement par un simple SELECT avant INSERT, sans verrou ni contrainte.
-- Deux exécutions concurrentes pour la même commande (ex. réouverture puis
-- re-clôture, webhook rejoué) peuvent toutes les deux passer ce test et
-- écrire chacune leur ligne : progression dupliquée/éparpillée pour un
-- même client, ou palier compté deux fois sur une même commande. Observé
-- sur staging (preuve : docs/decisions.md, discussion fidélité) avant
-- correction du code applicatif qui accompagne cette migration — ces deux
-- index rendent la double écriture impossible au niveau base, pas
-- seulement applicatif.
--
-- Vérifié sur staging avant d'écrire cette migration (2026-10-09) : 0
-- doublon restant sur les deux couples ci-dessous après la fusion des
-- clients dupliqués (cmd/merge_duplicate_customers). Cette migration
-- échouera normalement s'il en reste côté production — c'est volontaire :
-- mieux vaut un échec visible qu'un choix arbitraire de ligne à garder à
-- l'intérieur d'une migration. Dédupliquer avant de rejouer si besoin
-- (cmd/merge_duplicate_customers couvre le cas où la cause est un doublon
-- de client ; un doublon sur un client déjà unique nécessiterait un
-- nettoyage ad hoc, non couvert ici).
--
-- ATTENTION - CREATE UNIQUE INDEX CONCURRENTLY doit être joué hors bloc
-- transactionnel, comme les autres CREATE INDEX CONCURRENTLY de ce dépôt
-- (087, 109, 124, 132, 163, 172).
--
-- ORDRE DE DÉPLOIEMENT : appliquer APRÈS cmd/merge_duplicate_customers
-- (sinon risque d'échec sur des doublons liés à des clients dupliqués) et
-- STRICTEMENT AVANT le déploiement du code applicatif qui passe les
-- écritures en INSERT ... ON CONFLICT sur ces deux index — vérifié sur
-- staging (2026-10-09) : sans ces index, UpdateLoyaltyFromOrder échoue
-- immédiatement sur CHAQUE commande avec "there is no unique or exclusion
-- constraint matching the ON CONFLICT specification" (SQLSTATE 42P10).
-- ProcessOrderLoyalty avale cette erreur (échec silencieux déjà documenté,
-- docs/decisions.md) : la clôture de commande continuerait de fonctionner,
-- mais plus AUCUNE fidélité ne serait créditée jusqu'à l'application de
-- cette migration. Déployer le code avant la migration casse la fidélité
-- entière, pas seulement la portion concernée par la course.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_customer_loyalty_progress_customer_program
    ON customer_loyalty_progress (customer_id, loyalty_program_id);

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_customer_loyalty_progress_order_order_program
    ON customer_loyalty_progress_order (order_id, loyalty_program_id);
