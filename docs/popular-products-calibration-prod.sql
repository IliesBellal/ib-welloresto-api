-- Produits populaires (is_popular) : calibration de la refonte du calcul nocturne
-- Voir docs/POPULAR_PRODUCTS.md.
--
-- À exécuter sur la PROD, en lecture seule. Toutes les requêtes sont agrégées :
-- aucune donnée client (nom, téléphone, adresse, e-mail) n'est lue.
--
-- Les trois requêtes partagent la même définition du score (bloc WITH répété) :
--   - commandes valides : state CLOSED/DONE, brand_status hors CANCELED,
--     DENIED, DELETED, sur les 28 derniers jours ;
--   - une commande compte une fois par produit, quelle que soit la quantité ;
--   - chaque commande pèse 0.5 ^ (âge en jours / 14) ;
--   - le score est ramené en « commandes sur 28 jours au rythme actuel » :
--     un produit vendu régulièrement 1 fois par jour vaut environ 28 ;
--   - un produit créé il y a moins de 28 jours est extrapolé sur ses jours de
--     présence, avec un minimum de 7 jours ;
--   - catégorie d'affichage : celle du produit, sinon celle de son groupe
--     (variantes) ; seuls les produits affichables d'une catégorie active
--     sont candidats.
--
-- Seuls les établissements abonnés ayant au moins une commande valide sur la
-- période apparaissent dans les résultats.
--
-- Règle de sélection testée, par catégorie :
--   - rang <= plafond, avec plafond = 25 % des produits de la catégorie,
--     minimum 1, maximum 3 ;
--   - score >= seuil absolu (4, 6, 8 ou 12 selon la variante) ;
--   - à partir du 2e, score >= 50 % du score du 1er.


-- ─────────────────────────────────────────────────────────────────────────────
-- Requête 1 : synthèse, algo actuel contre nouvelles variantes
-- Une ligne par variante. Les moyennes sont calculées par établissement.
-- ─────────────────────────────────────────────────────────────────────────────
WITH
merchants AS (
  SELECT DISTINCT CAST(m.id AS TEXT) AS merchant_id
  FROM merchant m
  INNER JOIN subscriptions s ON s.merchant_id = CAST(m.id AS TEXT)
),
thresholds(variante, min_score) AS (
  VALUES ('nouveau, seuil 4', 4.0), ('nouveau, seuil 6', 6.0),
         ('nouveau, seuil 8', 8.0), ('nouveau, seuil 12', 12.0)
),
order_lines AS (
  SELECT DISTINCT o.merchant_id, o.order_id, o.creation_date, oi.product_id
  FROM orders o
  INNER JOIN merchants mm ON mm.merchant_id = o.merchant_id
  INNER JOIN orderitems oi ON oi.order_id = o.order_id
  WHERE o.creation_date >= now() - interval '28 days'
    AND o.state IN ('CLOSED', 'DONE')
    AND upper(o.brand_status) NOT IN ('CANCELED', 'DENIED', 'DELETED')
),
sales AS (
  SELECT merchant_id, product_id,
         SUM(power(0.5, extract(epoch FROM now() - creation_date) / 86400.0 / 14.0)) AS weighted
  FROM order_lines
  GROUP BY merchant_id, product_id
),
eligible AS (
  SELECT p.merchant_id, p.product_id, p.creation_date,
         COALESCE(p.is_popular, FALSE) AS is_popular,
         COALESCE(NULLIF(p.category, ''), g.category) AS category
  FROM products p
  INNER JOIN merchants mm ON mm.merchant_id = p.merchant_id
  LEFT JOIN products g
    ON g.product_id = p.by_product_of AND g.merchant_id = p.merchant_id
  WHERE p.enabled = TRUE
    AND p.status IN ('1', 'available')
    AND COALESCE(p.is_product_group, FALSE) = FALSE
    AND (
      p.by_product_of IS NULL OR p.by_product_of = 0
      OR (g.is_product_group = TRUE AND g.enabled = TRUE AND g.status IN ('1', 'available'))
    )
),
scored AS (
  SELECT e.merchant_id, e.product_id, e.category, e.is_popular,
         COALESCE(s.weighted, 0) * 28.0 * ln(2.0)
           / (14.0 * (1 - power(0.5, LEAST(28.0, GREATEST(7.0,
               extract(epoch FROM now() - e.creation_date) / 86400.0)) / 14.0))) AS score
  FROM eligible e
  INNER JOIN productcateg pc
    ON pc.merchant_id = e.merchant_id AND pc.merchant_categ_id = e.category AND pc.enabled = TRUE
  LEFT JOIN sales s ON s.merchant_id = e.merchant_id AND s.product_id = e.product_id
),
ranked AS (
  SELECT sc.*,
         ROW_NUMBER() OVER (PARTITION BY merchant_id, category ORDER BY score DESC, product_id) AS rk,
         COUNT(*)     OVER (PARTITION BY merchant_id, category) AS cat_size,
         MAX(score)   OVER (PARTITION BY merchant_id, category) AS leader
  FROM scored sc
),
selection AS (
  SELECT 'actuel' AS variante, merchant_id, category, cat_size, leader, is_popular AS selected
  FROM ranked
  UNION ALL
  SELECT t.variante, r.merchant_id, r.category, r.cat_size, r.leader,
         (r.rk <= LEAST(3, GREATEST(1, FLOOR(r.cat_size * 0.25)))
          AND r.score >= t.min_score
          AND (r.rk = 1 OR r.score >= 0.5 * r.leader)) AS selected
  FROM ranked r CROSS JOIN thresholds t
),
per_cat AS (
  SELECT variante, merchant_id, category, cat_size, MAX(leader) AS leader,
         COUNT(*) FILTER (WHERE selected) AS n_sel
  FROM selection
  GROUP BY variante, merchant_id, category, cat_size
),
per_merchant AS (
  SELECT variante, merchant_id,
         SUM(cat_size)                                   AS produits,
         COUNT(*)                                        AS categories,
         COUNT(*) FILTER (WHERE leader > 0)              AS categories_vendues,
         SUM(n_sel)                                      AS marques,
         COUNT(*) FILTER (WHERE n_sel > 0)               AS categories_couvertes,
         COUNT(*) FILTER (WHERE leader > 0 AND n_sel = 0) AS categories_vendues_sans_populaire,
         MAX(n_sel::numeric / cat_size)                  AS part_max
  FROM per_cat
  GROUP BY variante, merchant_id
)
SELECT variante,
       COUNT(*)                                                    AS etablissements,
       ROUND(AVG(produits), 1)                                     AS produits_moy,
       ROUND(AVG(categories), 1)                                   AS categories_moy,
       ROUND(AVG(marques), 1)                                      AS marques_moy,
       ROUND(100.0 * SUM(marques) / NULLIF(SUM(produits), 0), 1)   AS pct_produits_marques,
       ROUND(100.0 * SUM(categories_couvertes) / NULLIF(SUM(categories_vendues), 0), 1)
                                                                   AS pct_categories_vendues_couvertes,
       SUM(categories_vendues_sans_populaire)                      AS categories_vendues_sans_populaire,
       ROUND(100.0 * AVG(part_max), 1)                             AS part_max_moy_pct,
       COUNT(*) FILTER (WHERE part_max > 0.5)                      AS etab_avec_categorie_plus_50pct,
       COUNT(*) FILTER (WHERE marques = 0)                         AS etab_sans_aucun_populaire
FROM per_merchant
WHERE merchant_id IN (SELECT merchant_id FROM order_lines)
GROUP BY variante
ORDER BY variante;


-- ─────────────────────────────────────────────────────────────────────────────
-- Requête 2 : détail par établissement (même définition)
-- Une ligne par établissement et par variante, les plus gros volumes d'abord.
-- ─────────────────────────────────────────────────────────────────────────────
WITH
merchants AS (
  SELECT DISTINCT CAST(m.id AS TEXT) AS merchant_id
  FROM merchant m
  INNER JOIN subscriptions s ON s.merchant_id = CAST(m.id AS TEXT)
),
thresholds(variante, min_score) AS (
  VALUES ('nouveau, seuil 4', 4.0), ('nouveau, seuil 6', 6.0),
         ('nouveau, seuil 8', 8.0), ('nouveau, seuil 12', 12.0)
),
order_lines AS (
  SELECT DISTINCT o.merchant_id, o.order_id, o.creation_date, oi.product_id
  FROM orders o
  INNER JOIN merchants mm ON mm.merchant_id = o.merchant_id
  INNER JOIN orderitems oi ON oi.order_id = o.order_id
  WHERE o.creation_date >= now() - interval '28 days'
    AND o.state IN ('CLOSED', 'DONE')
    AND upper(o.brand_status) NOT IN ('CANCELED', 'DENIED', 'DELETED')
),
volume AS (
  SELECT merchant_id, COUNT(DISTINCT order_id) AS commandes_28j
  FROM order_lines
  GROUP BY merchant_id
),
sales AS (
  SELECT merchant_id, product_id,
         SUM(power(0.5, extract(epoch FROM now() - creation_date) / 86400.0 / 14.0)) AS weighted
  FROM order_lines
  GROUP BY merchant_id, product_id
),
eligible AS (
  SELECT p.merchant_id, p.product_id, p.creation_date,
         COALESCE(p.is_popular, FALSE) AS is_popular,
         COALESCE(NULLIF(p.category, ''), g.category) AS category
  FROM products p
  INNER JOIN merchants mm ON mm.merchant_id = p.merchant_id
  LEFT JOIN products g
    ON g.product_id = p.by_product_of AND g.merchant_id = p.merchant_id
  WHERE p.enabled = TRUE
    AND p.status IN ('1', 'available')
    AND COALESCE(p.is_product_group, FALSE) = FALSE
    AND (
      p.by_product_of IS NULL OR p.by_product_of = 0
      OR (g.is_product_group = TRUE AND g.enabled = TRUE AND g.status IN ('1', 'available'))
    )
),
scored AS (
  SELECT e.merchant_id, e.product_id, e.category, e.is_popular,
         COALESCE(s.weighted, 0) * 28.0 * ln(2.0)
           / (14.0 * (1 - power(0.5, LEAST(28.0, GREATEST(7.0,
               extract(epoch FROM now() - e.creation_date) / 86400.0)) / 14.0))) AS score
  FROM eligible e
  INNER JOIN productcateg pc
    ON pc.merchant_id = e.merchant_id AND pc.merchant_categ_id = e.category AND pc.enabled = TRUE
  LEFT JOIN sales s ON s.merchant_id = e.merchant_id AND s.product_id = e.product_id
),
ranked AS (
  SELECT sc.*,
         ROW_NUMBER() OVER (PARTITION BY merchant_id, category ORDER BY score DESC, product_id) AS rk,
         COUNT(*)     OVER (PARTITION BY merchant_id, category) AS cat_size,
         MAX(score)   OVER (PARTITION BY merchant_id, category) AS leader
  FROM scored sc
),
selection AS (
  SELECT 'actuel' AS variante, merchant_id, category, cat_size, leader, is_popular AS selected
  FROM ranked
  UNION ALL
  SELECT t.variante, r.merchant_id, r.category, r.cat_size, r.leader,
         (r.rk <= LEAST(3, GREATEST(1, FLOOR(r.cat_size * 0.25)))
          AND r.score >= t.min_score
          AND (r.rk = 1 OR r.score >= 0.5 * r.leader)) AS selected
  FROM ranked r CROSS JOIN thresholds t
),
per_cat AS (
  SELECT variante, merchant_id, category, cat_size, MAX(leader) AS leader,
         COUNT(*) FILTER (WHERE selected) AS n_sel
  FROM selection
  GROUP BY variante, merchant_id, category, cat_size
)
SELECT pc.merchant_id,
       v.commandes_28j,
       pc.variante,
       SUM(pc.cat_size)                                     AS produits,
       COUNT(*)                                             AS categories,
       COUNT(*) FILTER (WHERE pc.leader > 0)                AS categories_vendues,
       SUM(pc.n_sel)                                        AS marques,
       COUNT(*) FILTER (WHERE pc.n_sel > 0)                 AS categories_couvertes,
       ROUND(100.0 * MAX(pc.n_sel::numeric / pc.cat_size), 0) AS part_max_pct
FROM per_cat pc
INNER JOIN volume v ON v.merchant_id = pc.merchant_id
GROUP BY pc.merchant_id, v.commandes_28j, pc.variante
ORDER BY commandes_28j DESC, pc.merchant_id, pc.variante;


-- ─────────────────────────────────────────────────────────────────────────────
-- Requête 3 : ordre de grandeur des scores
-- Pour chaque catégorie qui a vendu, score du 1er, du 2e et du 3e. Répartition
-- sur toutes les catégories de tous les établissements (quartiles), pour
-- situer les seuils testés.
-- ─────────────────────────────────────────────────────────────────────────────
WITH
merchants AS (
  SELECT DISTINCT CAST(m.id AS TEXT) AS merchant_id
  FROM merchant m
  INNER JOIN subscriptions s ON s.merchant_id = CAST(m.id AS TEXT)
),
order_lines AS (
  SELECT DISTINCT o.merchant_id, o.order_id, o.creation_date, oi.product_id
  FROM orders o
  INNER JOIN merchants mm ON mm.merchant_id = o.merchant_id
  INNER JOIN orderitems oi ON oi.order_id = o.order_id
  WHERE o.creation_date >= now() - interval '28 days'
    AND o.state IN ('CLOSED', 'DONE')
    AND upper(o.brand_status) NOT IN ('CANCELED', 'DENIED', 'DELETED')
),
sales AS (
  SELECT merchant_id, product_id,
         SUM(power(0.5, extract(epoch FROM now() - creation_date) / 86400.0 / 14.0)) AS weighted
  FROM order_lines
  GROUP BY merchant_id, product_id
),
eligible AS (
  SELECT p.merchant_id, p.product_id, p.creation_date,
         COALESCE(NULLIF(p.category, ''), g.category) AS category
  FROM products p
  INNER JOIN merchants mm ON mm.merchant_id = p.merchant_id
  LEFT JOIN products g
    ON g.product_id = p.by_product_of AND g.merchant_id = p.merchant_id
  WHERE p.enabled = TRUE
    AND p.status IN ('1', 'available')
    AND COALESCE(p.is_product_group, FALSE) = FALSE
    AND (
      p.by_product_of IS NULL OR p.by_product_of = 0
      OR (g.is_product_group = TRUE AND g.enabled = TRUE AND g.status IN ('1', 'available'))
    )
),
scored AS (
  SELECT e.merchant_id, e.product_id, e.category,
         COALESCE(s.weighted, 0) * 28.0 * ln(2.0)
           / (14.0 * (1 - power(0.5, LEAST(28.0, GREATEST(7.0,
               extract(epoch FROM now() - e.creation_date) / 86400.0)) / 14.0))) AS score
  FROM eligible e
  INNER JOIN productcateg pc
    ON pc.merchant_id = e.merchant_id AND pc.merchant_categ_id = e.category AND pc.enabled = TRUE
  LEFT JOIN sales s ON s.merchant_id = e.merchant_id AND s.product_id = e.product_id
),
ranked AS (
  SELECT sc.*,
         ROW_NUMBER() OVER (PARTITION BY merchant_id, category ORDER BY score DESC, product_id) AS rk,
         MAX(score)   OVER (PARTITION BY merchant_id, category) AS leader
  FROM scored sc
)
SELECT rk AS rang,
       COUNT(*)                                                             AS categories,
       ROUND(percentile_cont(0.25) WITHIN GROUP (ORDER BY score)::numeric, 1) AS score_p25,
       ROUND(percentile_cont(0.50) WITHIN GROUP (ORDER BY score)::numeric, 1) AS score_median,
       ROUND(percentile_cont(0.75) WITHIN GROUP (ORDER BY score)::numeric, 1) AS score_p75,
       ROUND(percentile_cont(0.50) WITHIN GROUP (ORDER BY score / NULLIF(leader, 0))::numeric, 2)
                                                                            AS ratio_au_1er_median
FROM ranked
WHERE leader > 0 AND rk <= 3
GROUP BY rk
ORDER BY rk;
