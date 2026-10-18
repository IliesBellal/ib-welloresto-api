-- Audit upsell (2026-10) : requêtes de référence à exécuter sur la PROD
-- Voir docs/upsell/audit/L7-plan-mesure-profit.md (§2, « Mesures disponibles sans code »).
--
-- Lecture seule, résultats agrégés : aucune donnée client (nom, téléphone,
-- adresse, e-mail) n'est lue. Les charges utiles de api_request_logs ne sont
-- pas lues (seuls l'URL, le statut et la durée le sont).
-- Syntaxe et colonnes vérifiées par EXPLAIN sur staging le 2026-10-16
-- (plans seulement, aucune ligne lue ni exploitée).
--
-- Utilisation : psql "$PROD_URL" -f docs/upsell/audit/requetes-baseline-prod.sql > upsell-baseline.txt
-- puis transmettre le fichier de sortie.

BEGIN TRANSACTION READ ONLY;

-- B0 — Périmètre : volume par canal sur 90 jours et réglages upsell.
-- Donne le nombre de sessions potentiellement éligibles (question 1 de l'audit).
WITH ord AS (
  SELECT o.merchant_id,
         COUNT(*)                                                    AS closed_90d,
         COUNT(*) FILTER (WHERE o.order_source = 'SCANNORDER')       AS sno_90d,
         COUNT(*) FILTER (WHERE o.order_source = 'KIOSK')            AS kiosk_90d,
         COUNT(*) FILTER (WHERE o.order_source = 'WELLO_RESTO_POS')  AS pos_90d,
         COUNT(*) FILTER (WHERE o.order_source IN ('UBER_EATS', 'DELIVEROO')) AS marketplaces_90d
  FROM orders o
  WHERE o.state = 'CLOSED'
    AND o.creation_date >= now() - interval '90 days'
    AND COALESCE(upper(o.brand_status), '') NOT IN ('DELETED', 'CANCELED')
  GROUP BY o.merchant_id
  HAVING COUNT(*) >= 50
)
SELECT ord.merchant_id, ord.closed_90d, ord.sno_90d, ord.kiosk_90d, ord.pos_90d, ord.marketplaces_90d,
       COALESCE(mp.enable_upsell, FALSE) AS enable_upsell,
       mp.upsell_max_items,
       mp.pos_upsell_enabled
FROM ord
LEFT JOIN merchant_parameters mp ON mp.merchant_id = ord.merchant_id
ORDER BY ord.closed_90d DESC;

-- B1 — Suggestions générées par établissement, canal, source et semaine.
-- accepted_empty : lignes « acceptées » sans aucun article (constat C-03),
-- comptées aujourd'hui comme acceptées par l'onglet Analytics.
SELECT s.merchant_id,
       s.channel::text                                   AS channel,
       s.source,
       date_trunc('week', s.created_at)::date            AS week,
       COUNT(*)                                          AS rows_generated,
       ROUND(AVG(jsonb_array_length(s.suggested_items)), 2) AS avg_items,
       COUNT(*) FILTER (WHERE jsonb_array_length(s.suggested_items) = 0) AS empty_lists,
       COUNT(*) FILTER (WHERE s.accepted_items IS NOT NULL)              AS accepted_not_null,
       COUNT(*) FILTER (WHERE s.accepted_items IS NOT NULL
                          AND jsonb_array_length(s.accepted_items) = 0)  AS accepted_empty,
       COALESCE(SUM(s.revenue_impact), 0)                AS revenue_impact_ttc_eur
FROM upsell_suggestions s
WHERE s.created_at >= now() - interval '90 days'
GROUP BY 1, 2, 3, 4
ORDER BY 1, 2, 4, 3;

-- B2 — Taux d'acceptation par origine et par position dans la liste générée.
-- Attention : la position est celle de la liste générée par le moteur ; SNO et
-- la borne n'en affichent que 6 au plus et filtrent encore après le moteur
-- (constat C-21). Les positions > 6 n'ont jamais été vues sur ces canaux.
SELECT s.merchant_id,
       s.channel::text                     AS channel,
       COALESCE(it.item ->> 'origin', '?') AS origin,
       it.pos,
       COUNT(*)                            AS generated,
       COUNT(*) FILTER (WHERE EXISTS (
         SELECT 1
         FROM jsonb_array_elements(COALESCE(s.accepted_items, '[]'::jsonb)) a
         WHERE a ->> 'product_id' = it.item ->> 'product_id'
       ))                                  AS accepted
FROM upsell_suggestions s
CROSS JOIN LATERAL jsonb_array_elements(s.suggested_items) WITH ORDINALITY AS it(item, pos)
WHERE s.created_at >= now() - interval '90 days'
GROUP BY 1, 2, 3, 4
ORDER BY 1, 2, 3, 4;

-- B3 — Lignes vendues via une suggestion (is_upsell) : volume, CA TTC et
-- couverture du coût de revient figé (cost_price_unit). Les suppléments
-- (table extra) ne sont pas inclus : ordre de grandeur seulement.
SELECT o.merchant_id,
       o.order_source,
       COUNT(*)                                                         AS upsell_lines,
       SUM(oi.quantity)                                                 AS upsell_units,
       SUM(oi.price * oi.quantity)                                      AS upsell_revenue_ttc_cents,
       COUNT(*) FILTER (WHERE oi.cost_price_unit IS NOT NULL)           AS lines_cost_known,
       SUM(oi.cost_price_unit * oi.quantity) FILTER (WHERE oi.cost_price_unit IS NOT NULL) AS cost_known_cents,
       SUM(oi.price * oi.quantity) FILTER (WHERE oi.cost_price_unit IS NOT NULL)           AS revenue_ttc_cost_known_cents,
       COUNT(*) FILTER (WHERE oi.cost_price_reason = 'NO_RECIPE')         AS lines_no_recipe,
       COUNT(*) FILTER (WHERE oi.cost_price_reason = 'INCOMPLETE_RECIPE') AS lines_incomplete_recipe
FROM orderitems oi
JOIN orders o ON o.order_id = oi.order_id
WHERE oi.is_upsell = TRUE
  AND o.state IN ('CLOSED', 'DONE')
  AND COALESCE(upper(o.brand_status), '') NOT IN ('DELETED', 'CANCELED')
  AND o.creation_date >= now() - interval '90 days'
GROUP BY 1, 2
ORDER BY 1, 2;

-- B4 — Couverture du coût de revient des produits que le moteur propose.
-- Décide si un classement par marge est faisable avec les données actuelles
-- (question 2 de l'audit). never_evaluated : lignes écrites avant le lot
-- « coût de revient figé » (cost_price_unit et cost_price_reason à NULL).
WITH suggested AS (
  SELECT DISTINCT s.merchant_id, it ->> 'product_id' AS product_id
  FROM upsell_suggestions s
  CROSS JOIN LATERAL jsonb_array_elements(s.suggested_items) it
  WHERE s.created_at >= now() - interval '90 days'
),
lines AS (
  SELECT oi.merchant_id, oi.product_id::text AS product_id,
         oi.cost_price_unit, oi.cost_price_reason
  FROM orderitems oi
  JOIN orders o ON o.order_id = oi.order_id
  WHERE o.state = 'CLOSED'
    AND o.creation_date >= now() - interval '90 days'
)
SELECT sg.merchant_id,
       COUNT(DISTINCT sg.product_id)                                        AS suggested_products,
       COUNT(DISTINCT sg.product_id) FILTER (WHERE l.cost_price_unit IS NOT NULL) AS products_with_known_cost,
       COUNT(l.product_id)                                                  AS lines_90d,
       COUNT(l.cost_price_unit)                                             AS lines_cost_known,
       COUNT(*) FILTER (WHERE l.cost_price_reason = 'NO_RECIPE')            AS lines_no_recipe,
       COUNT(*) FILTER (WHERE l.cost_price_reason = 'INCOMPLETE_RECIPE')    AS lines_incomplete_recipe,
       COUNT(*) FILTER (WHERE l.product_id IS NOT NULL
                          AND l.cost_price_unit IS NULL
                          AND l.cost_price_reason IS NULL)                  AS lines_never_evaluated
FROM suggested sg
LEFT JOIN lines l ON l.merchant_id = sg.merchant_id AND l.product_id = sg.product_id
GROUP BY sg.merchant_id
ORDER BY sg.merchant_id;

-- B5 — Latence serveur des appels upsell (api_request_logs, rétention 90 jours).
-- over_client_timeout : appels plus longs que le délai des clients SNO et borne
-- (2 s) ; l'étape upsell est alors sautée sans trace côté moteur (constat C-15).
SELECT CASE
         WHEN url LIKE '%/scannorder/%' THEN 'SNO'
         WHEN url LIKE '%/kiosk/%'      THEN 'KIOSK'
         ELSE 'POS'
       END                                                          AS channel,
       COUNT(*)                                                     AS calls,
       percentile_cont(0.50) WITHIN GROUP (ORDER BY duration_ms)    AS p50_ms,
       percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms)    AS p95_ms,
       percentile_cont(0.99) WITHIN GROUP (ORDER BY duration_ms)    AS p99_ms,
       COUNT(*) FILTER (WHERE duration_ms > 2000)                   AS over_client_timeout,
       COUNT(*) FILTER (WHERE status_code <> 200)                   AS non_200
FROM api_request_logs
WHERE method = 'POST'
  AND created_at >= now() - interval '30 days'
  AND (url LIKE '%/orders/upsell%' OR url LIKE '%/kiosk/upsell%' OR url ~ '/scannorder/[^/]+/upsell')
GROUP BY 1
ORDER BY 1;

-- B6 — Couverture des sessions SNO et borne : listes non vides générées par
-- jour, rapportées aux commandes créées sur le même canal. Un ratio proche de 1
-- signifie que presque chaque commande passe par le popup (constat C-12).
-- Approximation : un client qui revient en arrière génère un second appel.
WITH sugg AS (
  SELECT merchant_id,
         CASE channel::text WHEN 'SNO' THEN 'SCANNORDER' ELSE 'KIOSK' END AS order_source,
         created_at::date AS day,
         COUNT(*) FILTER (WHERE jsonb_array_length(suggested_items) > 0) AS non_empty_lists,
         COUNT(*) AS calls
  FROM upsell_suggestions
  WHERE channel::text IN ('SNO', 'KIOSK')
    AND created_at >= now() - interval '90 days'
  GROUP BY 1, 2, 3
),
ord AS (
  SELECT merchant_id, order_source, creation_date::date AS day, COUNT(*) AS orders
  FROM orders
  WHERE order_source IN ('SCANNORDER', 'KIOSK')
    AND creation_date >= now() - interval '90 days'
  GROUP BY 1, 2, 3
)
SELECT s.merchant_id, s.order_source,
       SUM(s.calls)           AS calls,
       SUM(s.non_empty_lists) AS non_empty_lists,
       SUM(o.orders)          AS orders_same_days
FROM sugg s
LEFT JOIN ord o ON o.merchant_id = s.merchant_id AND o.order_source = s.order_source AND o.day = s.day
GROUP BY 1, 2
ORDER BY 1, 2;

-- B7 — Série hebdomadaire par canal : panier moyen et part des commandes avec
-- au moins une ligne is_upsell. Sert de point de comparaison avant/après
-- activation : corrélationnel, ne mesure PAS l'incrémentalité.
SELECT o.merchant_id,
       o.order_source,
       date_trunc('week', o.creation_date)::date               AS week,
       COUNT(*)                                                 AS orders,
       ROUND(AVG(o.price)::numeric, 0)                          AS avg_basket_ttc_cents,
       ROUND(AVG(o.ht)::numeric, 0)                             AS avg_basket_ht_cents,
       COUNT(*) FILTER (WHERE EXISTS (
         SELECT 1 FROM orderitems oi
         WHERE oi.order_id = o.order_id AND oi.is_upsell = TRUE
       ))                                                       AS orders_with_upsell_line
FROM orders o
WHERE o.state = 'CLOSED'
  AND COALESCE(upper(o.brand_status), '') NOT IN ('DELETED', 'CANCELED')
  AND o.order_source IN ('SCANNORDER', 'KIOSK', 'WELLO_RESTO_POS')
  AND o.creation_date >= now() - interval '180 days'
GROUP BY 1, 2, 3
ORDER BY 1, 2, 3;

-- B8 — État des commandes liées aux acceptations (constat C-22, ajouté le
-- 2026-10-16 après lecture de B1 et B3) : le suivi est écrit à la création de
-- la commande, avant paiement sur la borne et SNO. Une commande refusée,
-- annulée ou jamais payée garde son acceptation.
SELECT s.merchant_id,
       s.channel::text                          AS channel,
       o.state,
       COALESCE(upper(o.brand_status), '')      AS brand_status,
       o.cancelled_by_type,
       COUNT(*)                                 AS accepted_sessions,
       COALESCE(SUM(s.revenue_impact), 0)       AS revenue_impact_ttc_eur
FROM upsell_suggestions s
LEFT JOIN orders o ON o.order_id = s.order_id
WHERE s.created_at >= now() - interval '90 days'
  AND s.accepted_items IS NOT NULL
  AND jsonb_array_length(s.accepted_items) > 0
GROUP BY 1, 2, 3, 4, 5
ORDER BY 1, 2, 3, 4, 5;

ROLLBACK;
