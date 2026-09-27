-- Upsell : requêtes d'analyse pour le complément « boissons / desserts » (D3)
-- Voir docs/UPSELL_COMPLETION.md, points ouverts Q1 à Q4 et Q6.
--
-- À exécuter sur la PROD, en lecture seule. Toutes les requêtes sont agrégées :
-- aucune donnée client (nom, téléphone, adresse, e-mail) n'est lue.
-- Périmètre : établissements ayant au moins 50 commandes CLOSED sur 90 jours,
-- que l'upsell soit activé ou non (il ne l'est que sur une minorité).
-- Les variantes (by_product_of) sont rattachées à leur produit groupe, et le
-- prix d'un groupe (0 en base) est remplacé par celui de sa variante la moins
-- chère encore disponible.
--
-- Utilisation : psql "$PROD_URL" -f docs/upsell-analyse-prod.sql > upsell-analyse.txt
-- puis transmettre le fichier de sortie.

BEGIN TRANSACTION READ ONLY;

-- Q0 — Périmètre : établissements retenus, volume, réglages upsell actuels.
WITH ord AS (
  SELECT merchant_id, COUNT(*) AS orders_90d
  FROM orders
  WHERE state = 'CLOSED' AND creation_date >= now() - interval '90 days'
  GROUP BY merchant_id
  HAVING COUNT(*) >= 50
)
SELECT o.merchant_id, o.orders_90d,
       COALESCE(mp.enable_upsell, FALSE) AS enable_upsell,
       mp.upsell_max_items
FROM ord o
LEFT JOIN merchant_parameters mp ON mp.merchant_id = o.merchant_id
ORDER BY o.orders_90d DESC;

-- Q1 — Catégories : nom, taille, prix, ventes et part des paniers (Q1, Q2).
-- Sert à reconnaître les catégories boissons / desserts et leurs niveaux de prix.
WITH scope AS (
  SELECT merchant_id FROM orders
  WHERE state = 'CLOSED' AND creation_date >= now() - interval '90 days'
  GROUP BY merchant_id HAVING COUNT(*) >= 50
),
catalog AS (
  SELECT p.merchant_id, p.product_id, p.category,
         CASE WHEN p.is_product_group
              THEN (SELECT MIN(v.price) FROM products v
                    WHERE v.by_product_of = p.product_id AND v.available AND v.enabled)
              ELSE p.price END AS price
  FROM products p
  JOIN scope s ON s.merchant_id = p.merchant_id
  WHERE p.available AND p.enabled AND p.status IN ('available', '1')
    AND (p.by_product_of IS NULL OR p.by_product_of = 0)
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
orders_per_merchant AS (
  SELECT merchant_id, COUNT(DISTINCT order_id) AS n FROM lines GROUP BY merchant_id
),
cat_orders AS (
  SELECT l.merchant_id, g.category, COUNT(DISTINCT l.order_id) AS orders_with_cat
  FROM lines l JOIN products g ON g.product_id = l.product_id
  GROUP BY l.merchant_id, g.category
)
SELECT c.merchant_id, pc.categ_name, COUNT(*) AS products_available,
       MIN(c.price) AS price_min,
       percentile_cont(0.5) WITHIN GROUP (ORDER BY c.price) AS price_median,
       MAX(c.price) AS price_max,
       COALESCE(co.orders_with_cat, 0) AS orders_with_cat,
       round(100.0 * COALESCE(co.orders_with_cat, 0) / NULLIF(opm.n, 0), 1) AS pct_of_orders
FROM catalog c
LEFT JOIN productcateg pc ON pc.merchant_categ_id = c.category AND pc.merchant_id = c.merchant_id
LEFT JOIN cat_orders co ON co.merchant_id = c.merchant_id AND co.category = c.category
LEFT JOIN orders_per_merchant opm ON opm.merchant_id = c.merchant_id
GROUP BY c.merchant_id, pc.categ_name, co.orders_with_cat, opm.n
ORDER BY c.merchant_id, orders_with_cat DESC;

-- Q2 — Produits : ventes sur 90 jours et prix effectif, par catégorie (Q2, Q3).
-- Limité aux 40 meilleures ventes par établissement.
WITH scope AS (
  SELECT merchant_id FROM orders
  WHERE state = 'CLOSED' AND creation_date >= now() - interval '90 days'
  GROUP BY merchant_id HAVING COUNT(*) >= 50
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
sales AS (
  SELECT merchant_id, product_id, COUNT(*) AS orders_90d,
         ROW_NUMBER() OVER (PARTITION BY merchant_id ORDER BY COUNT(*) DESC) AS rank_
  FROM lines GROUP BY merchant_id, product_id
)
SELECT s.merchant_id, s.rank_, g.name, pc.categ_name,
       CASE WHEN g.is_product_group
            THEN (SELECT MIN(v.price) FROM products v
                  WHERE v.by_product_of = g.product_id AND v.available AND v.enabled)
            ELSE g.price END AS price,
       s.orders_90d,
       (g.available AND g.enabled AND g.status IN ('available', '1')) AS still_available
FROM sales s
JOIN products g ON g.product_id = s.product_id
LEFT JOIN productcateg pc ON pc.merchant_categ_id = g.category AND pc.merchant_id = g.merchant_id
WHERE s.rank_ <= 40
ORDER BY s.merchant_id, s.rank_;

-- Q3 — Catégories achetées ensemble (Q4) : parmi les commandes contenant la
-- catégorie A, part de celles qui contiennent aussi la catégorie B.
WITH scope AS (
  SELECT merchant_id FROM orders
  WHERE state = 'CLOSED' AND creation_date >= now() - interval '90 days'
  GROUP BY merchant_id HAVING COUNT(*) >= 50
),
cats AS (
  SELECT DISTINCT o.merchant_id, oi.order_id, COALESCE(pc.categ_name, '?') AS cat
  FROM orderitems oi
  JOIN orders o ON o.order_id = oi.order_id
  JOIN scope s ON s.merchant_id = o.merchant_id
  LEFT JOIN products p ON p.product_id = oi.product_id
  LEFT JOIN products g ON g.product_id = COALESCE(NULLIF(p.by_product_of, 0), oi.product_id)
  LEFT JOIN productcateg pc ON pc.merchant_categ_id = g.category AND pc.merchant_id = o.merchant_id
  WHERE o.state = 'CLOSED' AND o.creation_date >= now() - interval '90 days'
),
support AS (
  SELECT merchant_id, cat, COUNT(*) AS n FROM cats GROUP BY merchant_id, cat
)
SELECT a.merchant_id, a.cat AS cat_a, b.cat AS cat_b, COUNT(*) AS orders_ab, sa.n AS orders_a,
       round(100.0 * COUNT(*) / sa.n, 1) AS pct_a_with_b
FROM cats a
JOIN cats b ON b.merchant_id = a.merchant_id AND b.order_id = a.order_id AND b.cat <> a.cat
JOIN support sa ON sa.merchant_id = a.merchant_id AND sa.cat = a.cat
GROUP BY a.merchant_id, a.cat, b.cat, sa.n
HAVING COUNT(*) >= 10
ORDER BY a.merchant_id, orders_ab DESC;

-- Q4 — Taille des paniers (nombre de produits distincts par commande).
WITH scope AS (
  SELECT merchant_id FROM orders
  WHERE state = 'CLOSED' AND creation_date >= now() - interval '90 days'
  GROUP BY merchant_id HAVING COUNT(*) >= 50
),
baskets AS (
  SELECT o.merchant_id, oi.order_id, COUNT(DISTINCT oi.product_id) AS n
  FROM orderitems oi
  JOIN orders o ON o.order_id = oi.order_id
  JOIN scope s ON s.merchant_id = o.merchant_id
  WHERE o.state = 'CLOSED' AND o.creation_date >= now() - interval '90 days'
  GROUP BY o.merchant_id, oi.order_id
)
SELECT merchant_id, LEAST(n, 6) AS products_in_basket, COUNT(*) AS orders_
FROM baskets
GROUP BY merchant_id, LEAST(n, 6)
ORDER BY merchant_id, products_in_basket;

-- Q5 — Référence avant changement : suggestions upsell sur 90 jours, par
-- source et canal, nombre d'articles proposés et acceptations enregistrées.
SELECT s.merchant_id, s.source, s.channel,
       COUNT(*) AS suggestions,
       round(AVG(jsonb_array_length(s.suggested_items::jsonb)), 2) AS avg_items,
       MAX(mp.upsell_max_items) AS max_items_now,
       COUNT(*) FILTER (WHERE s.accepted_items IS NOT NULL
                          AND jsonb_array_length(s.accepted_items::jsonb) > 0) AS accepted
FROM upsell_suggestions s
LEFT JOIN merchant_parameters mp ON mp.merchant_id = s.merchant_id
WHERE s.created_at >= now() - interval '90 days'
GROUP BY s.merchant_id, s.source, s.channel
ORDER BY s.merchant_id, suggestions DESC;

ROLLBACK;
