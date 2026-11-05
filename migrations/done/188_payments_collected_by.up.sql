-- Encaisseur réel d'un paiement (docs/ids-entiers-users-discounts-chantier.md, §9).
--
-- payments.user_id est « qui a saisi le paiement » : il fait partie de la
-- charge utile signée (fiscal.PaymentPayload) et ne doit JAMAIS être réécrit
-- après l'insertion. Jusqu'ici, la livraison d'un arrêt réattribuait tous les
-- paiements de la commande au livreur en réécrivant user_id
-- (delivery_sessions.FinalizeDeliveredStop) : la signature du paiement ne
-- vérifiait plus (cmd/verify_fiscal).
--
-- collected_by porte désormais cette information, hors de la charge utile
-- signée (comme cash_register_id, rattaché après coup à la fermeture de la
-- caisse). NULL = l'encaisseur est celui de user_id. Renseignée uniquement par
-- FinalizeDeliveredStop, et seulement pour les paiements en main propre.
--
-- Lue par le détail de caisse (« encaissé par ») et par les paiements d'une
-- commande : COALESCE(collected_by, user_id).
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Les lectures ci-dessus
-- sélectionnent la colonne ; sans elle, le détail de caisse et la lecture des
-- commandes échouent.
--
-- Ajout d'une colonne nullable sans valeur par défaut : instantané, mais il
-- prend un verrou ACCESS EXCLUSIVE le temps de son acquisition. Sur une table
-- chaude, jouer avec SET lock_timeout = '3s' (et relancer en cas d'échec).

ALTER TABLE payments ADD COLUMN IF NOT EXISTS collected_by varchar(64);

COMMENT ON COLUMN payments.collected_by IS 'Encaisseur réel quand il diffère de user_id (livreur ayant encaissé en main propre). Hors charge utile signée. NULL = user_id.';
