-- PROMPT 26 Phase 2 — Réparer les compteurs client (customer_nb_orders,
-- customer_total_spent, last_order_date).
--
-- orders.customer_stats_counted_at est le marqueur d'idempotence qui permet
-- au crédit/retrait des compteurs client de tolérer : une reclôture externe
-- après clôture manuelle (SetDeliveredExternal ne vérifie pas OrderStillOpen
-- par conception — commentaire d'origine dans order_life_cycle/service.go),
-- une réouverture (ReopenClosedOrder) suivie d'une reclôture, et une course
-- entre deux clôtures concurrentes de la même commande — sans jamais compter
-- ou décompter deux fois la même commande. Voir docs/decisions.md (PROMPT 26
-- Phase 1/2) pour le diagnostic complet et internal/modules/customers/
-- repository.go's ApplyOrderToCustomerStats/ReverseOrderFromCustomerStats
-- pour l'usage exact.
--
-- Nullable, pas de défaut : NULL veut dire "cette commande ne contribue
-- actuellement à aucun compteur client" (jamais close, annulée après coup,
-- ou réouverte pour correction) ; non-NULL veut dire "actuellement comptée".
-- Migration additive pure, sans impact sur les lignes existantes tant que
-- Phase 3 (rattrapage) n'a pas tourné.
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS customer_stats_counted_at timestamptz;

COMMENT ON COLUMN orders.customer_stats_counted_at IS 'Non-NULL si cette commande contribue actuellement à customer.customer_nb_orders/customer_total_spent/last_order_date pour son client (marqueur d''idempotence — voir PROMPT 26). NULL si jamais comptée, annulée après coup, ou réouverte pour correction.';
