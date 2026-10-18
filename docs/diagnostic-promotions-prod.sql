-- Export lecture seule (production) des promotions, chantier « moteur de promotions ».
-- Voir docs/promotions-moteur-chantier.md (§ 5, protocole d'export).
--
-- But : transformer les promotions réelles en tests « golden » (chaque promotion
-- réelle + un panier type, résultat attendu AVANT et APRÈS la correction du moteur)
-- et recenser celles dont le résultat va changer.
--
-- Aucune donnée personnelle : pas de nom de promotion, de produit ni de marchand,
-- pas de code promo (seulement « a un code »), pas de client. Le marchand est
-- remplacé par une empreinte courte, stable d'une requête à l'autre (merchant_ref).
-- Lecture seule : à lancer dans une transaction READ ONLY (ci-dessous).
--
-- Les 5 requêtes sont indépendantes (aucun objet temporaire). Exporter chaque
-- résultat en CSV avec son numéro (A à E) et le transmettre tel quel.
--
-- « live » reproduit le filtre de orders.GetDiscounts : enabled, available,
-- valid_from <= aujourd'hui, valid_to NULL ou >= aujourd'hui. Les dates sont
-- comparées en UTC ici (le moteur compare en heure locale du marchand : un écart
-- d'un jour est possible autour de minuit, sans conséquence pour l'analyse).

BEGIN TRANSACTION READ ONLY;

-- ============================================================================
-- A. Une ligne par promotion non supprimée (enabled), avec les indicateurs
--    d'impact de la correction du moteur et l'usage des 90 derniers jours.
-- ============================================================================
WITH tgt AS (
    SELECT dp.discount_id_new,
           COUNT(*)                                           AS nb_produits_cibles,
           COUNT(*) FILTER (WHERE dp.new_price IS NULL)       AS nb_cibles_sans_prix,
           COUNT(*) FILTER (WHERE dp.new_price IS NOT NULL
                              AND dp.new_price >= p.price)    AS nb_cibles_prix_non_inferieur
    FROM discounts_products dp
    JOIN products p ON p.product_id = dp.product_id
    WHERE dp.enabled
    GROUP BY dp.discount_id_new
), opt AS (
    SELECT discount_id_new, COUNT(*) AS nb_contraintes_options
    FROM discounts_products_options
    GROUP BY discount_id_new
), sch AS (
    SELECT discount_id_new, COUNT(*) AS nb_creneaux
    FROM discounts_schedules
    WHERE enabled
    GROUP BY discount_id_new
), red AS (
    SELECT discount_id, COUNT(*) AS nb_utilisations_90j, SUM(amount_applied_cents) AS cents_remises_90j
    FROM discount_redemptions
    WHERE created_at >= now() - interval '90 days'
    GROUP BY discount_id
)
SELECT substr(md5(d.merchant_id::text || ':promo-audit'), 1, 8)          AS merchant_ref,
       d.discount_id_new,
       d.prefered_order,
       d.discount_scope,
       d.discount_unit,
       d.discount_value,
       d.discount_order_type,
       d.min_order_unit,
       d.min_order_value,
       d.max_discount_value,
       d.max_discount_unit,
       d.discounted_quantity,
       d.is_cumulative,
       d.is_time_limited,
       (d.discount_code IS NOT NULL)                                    AS has_code,
       d.available,
       d.valid_from::date                                               AS valid_from,
       d.valid_to::date                                                 AS valid_to,
       CASE WHEN NOT d.available                                   THEN 'inactive'
            WHEN d.valid_to IS NOT NULL AND d.valid_to::date < current_date THEN 'expired'
            WHEN d.valid_from::date > current_date                 THEN 'future'
            ELSE 'live' END                                             AS state,
       COALESCE(t.nb_produits_cibles, 0)                                AS nb_produits_cibles,
       COALESCE(t.nb_cibles_sans_prix, 0)                               AS nb_cibles_sans_prix,
       COALESCE(t.nb_cibles_prix_non_inferieur, 0)                      AS nb_cibles_prix_non_inferieur,
       COALESCE(o.nb_contraintes_options, 0)                            AS nb_contraintes_options,
       COALESCE(s.nb_creneaux, 0)                                       AS nb_creneaux,
       COALESCE(r.nb_utilisations_90j, 0)                               AS nb_utilisations_90j,
       COALESCE(r.cents_remises_90j, 0)                                 AS cents_remises_90j,
       -- Indicateurs d'impact (voir docs/promotions-moteur-chantier.md § 3)
       (d.is_cumulative AND upper(COALESCE(d.min_order_unit, '')) IN ('QTY', 'QUANTITY')
            AND d.min_order_value > d.discounted_quantity)              AS flag_cumulable_min_sup_remisee,   -- le résultat change
       (NOT d.is_cumulative AND COALESCE(t.nb_produits_cibles, 0) <> 1
            AND d.discounted_quantity < 99)                             AS flag_choix_unite_peut_changer,    -- P1 : l'unité remisée change
       (d.discount_unit = 'NEWPRICE'
            AND COALESCE(t.nb_cibles_prix_non_inferieur, 0) > 0)        AS flag_nouveau_prix_non_inferieur,  -- P2 / M5
       (d.discount_scope::text = 'ORDER_TOTAL')                         AS flag_portee_panier,               -- hors chantier, à connaître
       (d.discounted_quantity <= 0)                                     AS flag_quantite_remisee_nulle
FROM discounts d
LEFT JOIN tgt t ON t.discount_id_new = d.discount_id_new
LEFT JOIN opt o ON o.discount_id_new = d.discount_id_new
LEFT JOIN sch s ON s.discount_id_new = d.discount_id_new
LEFT JOIN red r ON r.discount_id     = d.discount_id_new
WHERE d.enabled
ORDER BY merchant_ref, d.prefered_order, d.discount_id_new;

-- ============================================================================
-- B. Produits ciblés par chaque promotion, avec le prix catalogue (centimes)
--    sur les trois canaux de prix du moteur et le prix remisé saisi.
-- ============================================================================
SELECT substr(md5(d.merchant_id::text || ':promo-audit'), 1, 8) AS merchant_ref,
       d.discount_id_new,
       dp.product_id,
       p.price,
       p.price_take_away,
       p.price_delivery,
       dp.new_price,
       p.is_product_group,
       p.available AS produit_disponible
FROM discounts d
JOIN discounts_products dp ON dp.discount_id_new = d.discount_id_new AND dp.enabled
JOIN products p ON p.product_id = dp.product_id
WHERE d.enabled
ORDER BY merchant_ref, d.discount_id_new, dp.product_id;

-- ============================================================================
-- C. Contraintes d'options (promotions qui exigent ou remisent une option).
-- ============================================================================
SELECT substr(md5(d.merchant_id::text || ':promo-audit'), 1, 8) AS merchant_ref,
       d.discount_id_new,
       dpo.product_id,
       dpo.option_id,
       dpo.is_option_mandatory,
       dpo.new_price
FROM discounts d
JOIN discounts_products_options dpo ON dpo.discount_id_new = d.discount_id_new
WHERE d.enabled
ORDER BY merchant_ref, d.discount_id_new, dpo.product_id, dpo.option_id;

-- ============================================================================
-- D. Créneaux horaires (day_of_week : lundi = 1 ; '00:00' en fin = minuit). Le
--    commentaire de colonne dit « UTC », mais le moteur compare ces heures à
--    l'heure LOCALE du marchand (orders.GetDiscounts) : lire les valeurs comme
--    de l'heure locale (10:45-14:00 = service du midi).
-- ============================================================================
SELECT substr(md5(d.merchant_id::text || ':promo-audit'), 1, 8) AS merchant_ref,
       d.discount_id_new,
       ds.day_of_week,
       ds.available_from,
       ds.available_to
FROM discounts d
JOIN discounts_schedules ds ON ds.discount_id_new = d.discount_id_new AND ds.enabled
WHERE d.enabled AND d.is_time_limited
ORDER BY merchant_ref, d.discount_id_new, ds.day_of_week, ds.available_from;

-- ============================================================================
-- E. Vue d'ensemble par « forme » de promotion (promotions live uniquement) :
--    permet de voir d'un coup quelles formes existent et lesquelles pèsent.
-- ============================================================================
SELECT d.discount_unit,
       d.discount_scope,
       d.is_cumulative,
       COALESCE(d.min_order_unit, '(aucun)')            AS min_order_unit,
       (d.discounted_quantity > 1)                      AS quantite_remisee_sup_1,
       (d.discount_code IS NOT NULL)                    AS has_code,
       d.is_time_limited,
       COUNT(*)                                         AS nb_promotions,
       COUNT(DISTINCT d.merchant_id)                    AS nb_marchands
FROM discounts d
WHERE d.enabled AND d.available
  AND d.valid_from::date <= current_date
  AND (d.valid_to IS NULL OR d.valid_to::date >= current_date)
GROUP BY 1, 2, 3, 4, 5, 6, 7
ORDER BY nb_promotions DESC;

-- ============================================================================
-- F. (complément 2026-10-17) Prix réellement facturés, sur les 180 derniers jours,
--    pour les lignes dont le produit est une formule (is_product_group) ou dont le
--    prix fixe n'est pas inférieur au prix du catalogue. Répond à la question :
--    une formule à 0 € ciblée par un prix fixe est-elle bien facturée au prix fixe
--    (cas P2 voulu par le commerçant) ou est-ce une anomalie ?
--    Aucune commande, aucun client : agrégats par promotion et produit.
-- ============================================================================
SELECT substr(md5(d.merchant_id::text || ':promo-audit'), 1, 8) AS merchant_ref,
       d.discount_id_new,
       oi.product_id,
       p.is_product_group,
       dp.new_price                                             AS prix_fixe_promotion,
       p.price                                                  AS prix_catalogue,
       COUNT(*)                                                 AS nb_lignes,
       SUM(oi.quantity)                                         AS nb_unites,
       MIN(oi.base_price)                                       AS base_price_min,
       MAX(oi.base_price)                                       AS base_price_max,
       MIN(oi.price)                                            AS price_min,
       MAX(oi.price)                                            AS price_max
FROM orderitems oi
JOIN discounts d           ON d.discount_id_new = oi.discount_id AND d.enabled
JOIN discounts_products dp ON dp.discount_id_new = d.discount_id_new
                          AND dp.product_id = oi.product_id AND dp.enabled
JOIN products p            ON p.product_id = oi.product_id
WHERE oi.ordered_on >= now() - interval '180 days'
  AND (p.is_product_group OR dp.new_price >= p.price)
GROUP BY 1, 2, 3, 4, 5, 6
ORDER BY merchant_ref, d.discount_id_new, oi.product_id;

-- ============================================================================
-- G. (complément 2026-10-17) Le premier export a été tronqué : relancer B et C
--    limités aux promotions 9 et 10 (34 produits ciblés chacune, 34 contraintes
--    d'options, et la promotion 10 n'est apparue dans aucun des deux).
--    Si le prochain export n'est pas tronqué, cette section est inutile.
-- ============================================================================
SELECT 'B' AS source, d.discount_id_new, dp.product_id::text AS product_id,
       NULL::text AS option_id, p.price, p.price_take_away, p.price_delivery,
       dp.new_price, p.is_product_group, NULL::boolean AS is_option_mandatory
FROM discounts d
JOIN discounts_products dp ON dp.discount_id_new = d.discount_id_new AND dp.enabled
JOIN products p ON p.product_id = dp.product_id
WHERE d.enabled AND d.discount_id_new IN (9, 10)
UNION ALL
SELECT 'C', d.discount_id_new, dpo.product_id::text, dpo.option_id::text,
       NULL, NULL, NULL, dpo.new_price, NULL, dpo.is_option_mandatory
FROM discounts d
JOIN discounts_products_options dpo ON dpo.discount_id_new = d.discount_id_new
WHERE d.enabled AND d.discount_id_new IN (9, 10)
ORDER BY 1, 2, 3, 4;

ROLLBACK;
