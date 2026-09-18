-- =====================================================================
-- SUITE — Investigation des 6 commandes désynchronisées d'août 2026
-- (merchant 212, Croq'Ô'Pizzas). 100 % READ-ONLY.
--
-- Objectif : déterminer, pour chaque commande, si orders.price ou la somme
-- des lignes (orderitems + extra) reflète ce qui a réellement été
-- vendu/encaissé, AVANT d'écrire le moindre UPDATE.
-- =====================================================================

BEGIN;
SET TRANSACTION READ ONLY;

-- 1. Détail complet des 6 commandes en cause
SELECT order_id, creation_date AT TIME ZONE 'Europe/Paris' AS date_locale,
       state, brand_status, order_type, created_by,
       price, ht, tva, delivery_fees,
       cart_discount_amount, cart_discount_code,
       isPaid, means_of_payement, monnaie,
       deletion_reason_id, deletion_comment,
       last_update AT TIME ZONE 'Europe/Paris' AS last_update_locale,
       hash IS NOT NULL AS a_un_hash
FROM orders
WHERE order_id IN (35473, 34236, 35588, 34604, 35819, 34681)
ORDER BY order_id;

-- 2. Lignes de commande (orderitems) de ces 6 commandes
SELECT oi.order_id, oi.order_item_id, p.name, oi.quantity, oi.price,
       oi.base_price, oi.discount_id, oi.isPaid AS ligne_payee,
       oi.production_status
FROM orderitems oi
JOIN products p ON p.product_id = oi.product_id
WHERE oi.order_id IN (35473, 34236, 35588, 34604, 35819, 34681)
ORDER BY oi.order_id, oi.order_item_id;

-- 3. Extras rattachés à ces lignes
SELECT ex.order_id, ex.order_item_id, ex.quantity, ex.price
FROM extra ex
WHERE ex.order_id IN (35473, 34236, 35588, 34604, 35819, 34681)
ORDER BY ex.order_id, ex.order_item_id;

-- 4. Paiements enregistrés sur ces commandes — LA preuve de ce qui a été
--    réellement encaissé, indépendamment de orders.price et des lignes.
SELECT p.order_id, p.payment_id, p.amount, p.mop, p.enabled,
       p.operation_type, p.status_check,
       p.payment_date AT TIME ZONE 'Europe/Paris' AS date_locale,
       p.cash_register_id
FROM payments p
WHERE p.order_id IN (35473, 34236, 35588, 34604, 35819, 34681)
ORDER BY p.order_id, p.payment_id;

-- 5. Le registre de caisse auquel chaque commande est rattachée, et son
--    état (pour savoir si le fix "réel" a déjà digéré ces montants)
SELECT o.order_id, o.cash_register_id, cr.enclosed, cr.closed,
       cr.start_date AT TIME ZONE 'Europe/Paris' AS debut_local,
       cr.end_date AT TIME ZONE 'Europe/Paris' AS fin_local
FROM orders o
LEFT JOIN cash_registers cr ON cr.cash_register_id::text = o.cash_register_id
WHERE o.order_id IN (35473, 34236, 35588, 34604, 35819, 34681)
ORDER BY o.order_id;

-- 6. Ampleur du phénomène : combien de commandes CLOSED, tous merchants,
--    ont un orders.price qui ne correspond pas à leurs lignes + frais de
--    port, sur les 12 derniers mois — pour savoir si c'est un incident
--    isolé à Croq'Ô'Pizzas ou un bug systémique actif partout.
WITH l AS (
  SELECT o.order_id, o.merchant_id, o.creation_date, o.price, o.delivery_fees,
         COALESCE(SUM((oi.price + COALESCE(e.extra_price,0)) * oi.quantity), 0) AS items
  FROM orders o
  JOIN orderitems oi ON oi.order_id = o.order_id
  LEFT JOIN (SELECT order_item_id, SUM(price) AS extra_price
             FROM extra GROUP BY order_item_id) e
         ON e.order_item_id = oi.order_item_id
  WHERE o.state = 'CLOSED' AND o.brand = 'WELLO_RESTO'
    AND o.brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED')
    AND o.creation_date >= now() - interval '12 months'
  GROUP BY 1,2,3,4,5
)
SELECT to_char(creation_date AT TIME ZONE 'Europe/Paris','YYYY-MM') AS mois,
       merchant_id,
       count(*) AS nb_commandes_desync,
       ROUND(SUM(items + delivery_fees - price)/100.0, 2) AS ecart_total_eur
FROM l
WHERE price <> items + delivery_fees
GROUP BY 1,2
ORDER BY 1 DESC, 4 DESC;

ROLLBACK;
