-- =====================================================================
-- CORRECTIF DE DONNÉES — orders.price/ht/tva désynchronisés de leurs lignes
-- Merchant 212 (Croq'Ô'Pizzas), 6 commandes identifiées le 2026-09-18
-- (cf. docs/diagnostic-rapport-comptable-croq-o-pizzas.sql, section 3).
--
-- RÈGLE APPLIQUÉE : identique à computeOrderTotals
-- (internal/modules/order_life_cycle/repository.go) — orders.price/ht/tva
-- sont désormais dérivés des lignes (orderitems + extra + delivery_fees),
-- jamais l'inverse. Ce script aligne rétroactivement les commandes déjà
-- créées sur cette même règle.
--   ligne_ttc = (orderitems.price + SUM(extra.price sur cette ligne)) * quantity
--   ligne_ht  = ROUND(ligne_ttc * 100 / (100 + tva_rate))
--   price     = SUM(ligne_ttc) + delivery_fees
--   ht        = SUM(ligne_ht) + ROUND(delivery_fees * 100/(100+20)) si delivery_fees<>0
--   tva       = price - ht
--
-- ATTENTION :
--   - orders.hash/signature/previous_hash (chaînage fiscal) ne sont PAS
--     recalculés par ce script : le hash existant devient incohérent avec
--     le nouveau price. Accepté explicitement pour l'instant (2026-09-18) —
--     à traiter dans un lot séparé si un ré-enchaînement est nécessaire.
--   - N'écrit RIEN tant que vous n'avez pas tapé COMMIT vous-même en fin de
--     script, après avoir vérifié les deux SELECT de contrôle.
--   - Scope volontairement restreint aux 6 commandes déjà identifiées.
--     Si docs/diagnostic-rapport-comptable-suite.sql (section 6) révèle
--     d'autres commandes/marchands touchés, dupliquez ce script avec la
--     nouvelle liste plutôt que d'élargir celui-ci à l'aveugle.
-- =====================================================================

BEGIN;

-- ---------------------------------------------------------------------
-- 1. APERÇU avant correction — vérifiez ces chiffres avant de continuer
-- ---------------------------------------------------------------------
WITH cible AS (
  SELECT order_id FROM (VALUES (35473),(34236),(35588),(34604),(35819),(34681)) v(order_id)
),
lignes AS (
  SELECT o.order_id,
         ((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity)::numeric AS ligne_ttc,
         ROUND(((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity)::numeric
               * 100.0 / (100.0 + tva.tva_rate)) AS ligne_ht
  FROM orders o
  JOIN cible c ON c.order_id = o.order_id
  JOIN orderitems oi ON oi.order_id = o.order_id
  JOIN products p ON p.product_id = oi.product_id
  JOIN tva_categories tva ON tva.tva_id = (CASE
      WHEN o.order_type = 'DELIVERY'  THEN p.tva_delivery_id
      WHEN o.order_type = 'TAKE_AWAY' THEN p.tva_take_away_id
      ELSE p.tva_in_id END)
  LEFT JOIN (SELECT order_item_id, SUM(price) AS extra_price
             FROM extra GROUP BY order_item_id) e
         ON e.order_item_id = oi.order_item_id
),
agrege AS (
  SELECT order_id, SUM(ligne_ttc) AS items_ttc, SUM(ligne_ht) AS items_ht
  FROM lignes GROUP BY order_id
),
frais AS (
  SELECT o.order_id,
         CASE WHEN o.delivery_fees <> 0
              THEN ROUND(o.delivery_fees * 100.0 / (100.0 + COALESCE(tf.tva_rate, 0)))
              ELSE 0 END AS fees_ht
  FROM orders o
  JOIN cible c ON c.order_id = o.order_id
  LEFT JOIN tva_categories tf ON tf.tva_id = -1
)
SELECT o.order_id,
       o.price AS ancien_price, o.ht AS ancien_ht, o.tva AS ancien_tva,
       (a.items_ttc + o.delivery_fees)::int AS nouveau_price,
       (a.items_ht + f.fees_ht)::int AS nouveau_ht,
       (a.items_ttc + o.delivery_fees - a.items_ht - f.fees_ht)::int AS nouveau_tva
FROM orders o
JOIN agrege a ON a.order_id = o.order_id
JOIN frais f ON f.order_id = o.order_id
ORDER BY o.order_id;

-- ---------------------------------------------------------------------
-- 2. CORRECTION — n'exécutez cette étape qu'après avoir validé l'aperçu
--    ci-dessus (nouveau_price doit correspondre à ce que vous attendez).
-- ---------------------------------------------------------------------
WITH cible AS (
  SELECT order_id FROM (VALUES (35473),(34236),(35588),(34604),(35819),(34681)) v(order_id)
),
lignes AS (
  SELECT o.order_id,
         ((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity)::numeric AS ligne_ttc,
         ROUND(((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity)::numeric
               * 100.0 / (100.0 + tva.tva_rate)) AS ligne_ht
  FROM orders o
  JOIN cible c ON c.order_id = o.order_id
  JOIN orderitems oi ON oi.order_id = o.order_id
  JOIN products p ON p.product_id = oi.product_id
  JOIN tva_categories tva ON tva.tva_id = (CASE
      WHEN o.order_type = 'DELIVERY'  THEN p.tva_delivery_id
      WHEN o.order_type = 'TAKE_AWAY' THEN p.tva_take_away_id
      ELSE p.tva_in_id END)
  LEFT JOIN (SELECT order_item_id, SUM(price) AS extra_price
             FROM extra GROUP BY order_item_id) e
         ON e.order_item_id = oi.order_item_id
),
agrege AS (
  SELECT order_id, SUM(ligne_ttc) AS items_ttc, SUM(ligne_ht) AS items_ht
  FROM lignes GROUP BY order_id
),
frais AS (
  SELECT o.order_id,
         CASE WHEN o.delivery_fees <> 0
              THEN ROUND(o.delivery_fees * 100.0 / (100.0 + COALESCE(tf.tva_rate, 0)))
              ELSE 0 END AS fees_ht
  FROM orders o
  JOIN cible c ON c.order_id = o.order_id
  LEFT JOIN tva_categories tf ON tf.tva_id = -1
),
correction AS (
  SELECT o.order_id,
         (a.items_ttc + o.delivery_fees)::int AS nouveau_price,
         (a.items_ht + f.fees_ht)::int AS nouveau_ht,
         (a.items_ttc + o.delivery_fees - a.items_ht - f.fees_ht)::int AS nouveau_tva
  FROM orders o
  JOIN agrege a ON a.order_id = o.order_id
  JOIN frais f ON f.order_id = o.order_id
)
UPDATE orders o
SET price       = correction.nouveau_price,
    ht          = correction.nouveau_ht,
    tva         = correction.nouveau_tva,
    last_update = now()
FROM correction
WHERE o.order_id = correction.order_id;

-- ---------------------------------------------------------------------
-- 3. VÉRIFICATION post-correction — ancien_price doit maintenant valoir
--    ce qui était "nouveau_price" à l'étape 1, pour les 6 commandes.
-- ---------------------------------------------------------------------
SELECT order_id, price, ht, tva, last_update AT TIME ZONE 'Europe/Paris' AS maj_locale
FROM orders
WHERE order_id IN (35473, 34236, 35588, 34604, 35819, 34681)
ORDER BY order_id;

-- Si tout est correct : COMMIT;
-- Pour annuler sans rien écrire   : ROLLBACK;
