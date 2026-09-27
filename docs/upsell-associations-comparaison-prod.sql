-- Upsell : associations « achetés ensemble », anciennes règles vs règles durcies (D12)
-- Voir docs/UPSELL_COMPLETION.md, §8.
--
-- À exécuter sur la PROD, en lecture seule. Requêtes agrégées : aucune donnée
-- client n'est lue. Périmètre : établissements ayant au moins 200 commandes
-- CLOSED sur 90 jours. Les variantes sont rattachées à leur produit groupe,
-- comme dans le cron.
--
-- Les seuils sont dans le bloc « params » de chaque requête : on peut les
-- modifier pour essayer d'autres valeurs (les deux requêtes doivent garder
-- les mêmes).
--   Anciennes règles (avant D12) : au moins 5 commandes ensemble, lift ≥ 1,0,
--                      P(B | A) ≥ 10 %, classement par lift.
--   Règles retenues (D12) : au moins
--                      8 commandes ensemble, lift ≥ 1,2, P(B | A) lissée ≥ 10 %,
--                      classement par P(B | A) lissée. (Première proposition
--                      à 15 %, jugée trop stricte sur les résultats de prod.)
--   Non reproduit ici : le moteur écarte en plus, au moment de proposer, les
--   suggestions d'une catégorie déjà présente dans le panier (voir la colonne
--   meme_categorie pour s'en faire une idée).
-- P(B | A) lissée = (commandes A+B + alpha × part de B) / (commandes A + alpha) :
-- sur peu de commandes, elle est ramenée vers la fréquence moyenne de B, ce
-- qui évite qu'un 6 sur 6 passe pour une certitude.
--
-- Utilisation : psql "$PROD_URL" -f docs/upsell-associations-comparaison-prod.sql > upsell-associations.txt

BEGIN TRANSACTION READ ONLY;

-- A1 — Pour les 10 produits les plus vendus de chaque établissement : les
-- 6 premières suggestions selon les anciennes règles (« actuel ») puis selon les règles
-- proposées.
WITH params AS (
  SELECT 5 AS cur_min_cab, 1.0 AS cur_min_lift, 0.10 AS cur_min_conf,
         8 AS new_min_cab, 1.2 AS new_min_lift, 0.10 AS new_min_conf,
         10.0 AS alpha
),
scope AS (
  SELECT merchant_id, COUNT(*) AS n FROM orders
  WHERE state = 'CLOSED' AND creation_date >= now() - interval '90 days'
  GROUP BY merchant_id HAVING COUNT(*) >= 200
),
lines AS (
  SELECT DISTINCT o.merchant_id, oi.order_id,
         COALESCE(NULLIF(p.by_product_of, 0), oi.product_id) AS product_id
  FROM orderitems oi
  JOIN orders o ON o.order_id = oi.order_id
  JOIN scope s ON s.merchant_id = o.merchant_id
  LEFT JOIN products p ON p.product_id = oi.product_id
  WHERE o.state = 'CLOSED' AND o.creation_date >= now() - interval '90 days'
),
sup AS (
  SELECT merchant_id, product_id, COUNT(*) AS c FROM lines GROUP BY merchant_id, product_id
),
pairs AS (
  SELECT a.merchant_id, a.product_id AS pa, b.product_id AS pb, COUNT(*) AS cab
  FROM lines a
  JOIN lines b ON b.merchant_id = a.merchant_id AND b.order_id = a.order_id
              AND b.product_id <> a.product_id
  GROUP BY a.merchant_id, a.product_id, b.product_id
  HAVING COUNT(*) >= (SELECT LEAST(cur_min_cab, new_min_cab) FROM params)
),
m AS (
  SELECT p.merchant_id, p.pa, p.pb, p.cab, sa.c AS ca,
         p.cab::float * s.n / (sa.c::float * sb.c)                   AS lift,
         p.cab::float / sa.c                                           AS conf,
         (p.cab + pr.alpha * sb.c::float / s.n) / (sa.c + pr.alpha)   AS conf_s
  FROM pairs p
  JOIN sup sa ON sa.merchant_id = p.merchant_id AND sa.product_id = p.pa
  JOIN sup sb ON sb.merchant_id = p.merchant_id AND sb.product_id = p.pb
  JOIN scope s ON s.merchant_id = p.merchant_id
  CROSS JOIN params pr
),
avail AS (
  SELECT p.product_id FROM products p
  JOIN scope s ON s.merchant_id = p.merchant_id
  WHERE p.available AND p.enabled AND p.status IN ('available', '1')
    AND (p.by_product_of IS NULL OR p.by_product_of = 0)
),
ranked AS (
  SELECT m.*, 'actuel' AS regle,
         ROW_NUMBER() OVER (PARTITION BY m.merchant_id, m.pa ORDER BY m.lift DESC, m.pb) AS rang
  FROM m CROSS JOIN params pr
  WHERE m.cab >= pr.cur_min_cab AND m.lift >= pr.cur_min_lift AND m.conf >= pr.cur_min_conf
    AND m.pb IN (SELECT product_id FROM avail)
  UNION ALL
  SELECT m.*, 'propose' AS regle,
         ROW_NUMBER() OVER (PARTITION BY m.merchant_id, m.pa ORDER BY m.conf_s DESC, m.pb) AS rang
  FROM m CROSS JOIN params pr
  WHERE m.cab >= pr.new_min_cab AND m.lift >= pr.new_min_lift AND m.conf_s >= pr.new_min_conf
    AND m.pb IN (SELECT product_id FROM avail)
),
sources AS (
  SELECT merchant_id, product_id,
         ROW_NUMBER() OVER (PARTITION BY merchant_id ORDER BY c DESC, product_id) AS rk
  FROM sup
)
SELECT r.merchant_id, src.rk AS rang_source, pa.name AS produit_panier,
       r.regle, r.rang, pb.name AS suggestion, pc.categ_name AS categorie_suggestion,
       (pa.category = pb.category) AS meme_categorie,
       CASE WHEN pb.is_product_group
            THEN (SELECT MIN(v.price) FROM products v
                  WHERE v.by_product_of = pb.product_id AND v.available AND v.enabled)
            ELSE pb.price END AS prix,
       r.cab AS commandes_ensemble, r.ca AS commandes_produit_panier,
       round((100 * r.conf)::numeric, 1)   AS pct_ajout,
       round((100 * r.conf_s)::numeric, 1) AS pct_ajout_lisse,
       round(r.lift::numeric, 2)           AS lift
FROM ranked r
JOIN sources src ON src.merchant_id = r.merchant_id AND src.product_id = r.pa AND src.rk <= 10
JOIN products pa ON pa.product_id = r.pa
JOIN products pb ON pb.product_id = r.pb
LEFT JOIN productcateg pc ON pc.merchant_categ_id = pb.category AND pc.merchant_id = pb.merchant_id
WHERE r.rang <= 6
ORDER BY r.merchant_id, src.rk, r.regle, r.rang;

-- A2 — Couverture : combien de produits ont au moins 1 et au moins 3
-- suggestions, selon chaque règle. Des règles plus strictes donnent moins
-- d'associations ; les places libérées sont complétées par les petits prix.
WITH params AS (
  SELECT 5 AS cur_min_cab, 1.0 AS cur_min_lift, 0.10 AS cur_min_conf,
         8 AS new_min_cab, 1.2 AS new_min_lift, 0.10 AS new_min_conf,
         10.0 AS alpha
),
scope AS (
  SELECT merchant_id, COUNT(*) AS n FROM orders
  WHERE state = 'CLOSED' AND creation_date >= now() - interval '90 days'
  GROUP BY merchant_id HAVING COUNT(*) >= 200
),
lines AS (
  SELECT DISTINCT o.merchant_id, oi.order_id,
         COALESCE(NULLIF(p.by_product_of, 0), oi.product_id) AS product_id
  FROM orderitems oi
  JOIN orders o ON o.order_id = oi.order_id
  JOIN scope s ON s.merchant_id = o.merchant_id
  LEFT JOIN products p ON p.product_id = oi.product_id
  WHERE o.state = 'CLOSED' AND o.creation_date >= now() - interval '90 days'
),
sup AS (
  SELECT merchant_id, product_id, COUNT(*) AS c FROM lines GROUP BY merchant_id, product_id
),
pairs AS (
  SELECT a.merchant_id, a.product_id AS pa, b.product_id AS pb, COUNT(*) AS cab
  FROM lines a
  JOIN lines b ON b.merchant_id = a.merchant_id AND b.order_id = a.order_id
              AND b.product_id <> a.product_id
  GROUP BY a.merchant_id, a.product_id, b.product_id
  HAVING COUNT(*) >= (SELECT LEAST(cur_min_cab, new_min_cab) FROM params)
),
m AS (
  SELECT p.merchant_id, p.pa, p.pb, p.cab,
         p.cab::float * s.n / (sa.c::float * sb.c)                   AS lift,
         p.cab::float / sa.c                                           AS conf,
         (p.cab + pr.alpha * sb.c::float / s.n) / (sa.c + pr.alpha)   AS conf_s
  FROM pairs p
  JOIN sup sa ON sa.merchant_id = p.merchant_id AND sa.product_id = p.pa
  JOIN sup sb ON sb.merchant_id = p.merchant_id AND sb.product_id = p.pb
  JOIN scope s ON s.merchant_id = p.merchant_id
  CROSS JOIN params pr
),
per_source AS (
  SELECT m.merchant_id, m.pa,
         COUNT(*) FILTER (WHERE m.cab >= pr.cur_min_cab AND m.lift >= pr.cur_min_lift AND m.conf >= pr.cur_min_conf) AS n_actuel,
         COUNT(*) FILTER (WHERE m.cab >= pr.new_min_cab AND m.lift >= pr.new_min_lift AND m.conf_s >= pr.new_min_conf) AS n_propose
  FROM m CROSS JOIN params pr
  GROUP BY m.merchant_id, m.pa
)
SELECT s.merchant_id, s.n AS commandes_90j,
       (SELECT COUNT(*) FROM sup WHERE sup.merchant_id = s.merchant_id) AS produits_vendus,
       COUNT(*) FILTER (WHERE ps.n_actuel  >= 1) AS avec_1_actuel,
       COUNT(*) FILTER (WHERE ps.n_propose >= 1) AS avec_1_propose,
       COUNT(*) FILTER (WHERE ps.n_actuel  >= 3) AS avec_3_actuel,
       COUNT(*) FILTER (WHERE ps.n_propose >= 3) AS avec_3_propose
FROM scope s
LEFT JOIN per_source ps ON ps.merchant_id = s.merchant_id
GROUP BY s.merchant_id, s.n
ORDER BY s.n DESC;

ROLLBACK;
