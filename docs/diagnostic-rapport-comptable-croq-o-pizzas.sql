-- =====================================================================
-- DIAGNOSTIC — Rapport comptable Croq'Ô'Pizzas (merchant 212)
-- Juillet & Août 2026 — 100 % READ-ONLY
--
-- Exécuter d'un bloc. Chaque section est numérotée et son résultat
-- attendu / son interprétation sont décrits en commentaire.
--
-- Bornes : le rapport PDF travaille en heure locale Europe/Paris.
-- En juillet/août 2026 Paris = UTC+2, donc :
--   Juillet : [2026-06-30 22:00Z , 2026-07-31 22:00Z[
--   Août    : [2026-07-31 22:00Z , 2026-08-31 22:00Z[
-- =====================================================================

BEGIN;
SET TRANSACTION READ ONLY;

-- ---------------------------------------------------------------------
-- 0. Contrôles d'environnement
-- ---------------------------------------------------------------------
SHOW timezone;   -- doit être UTC ; sinon toutes les bornes du code sont décalées

SELECT id, fullname, siret, timezone FROM merchant WHERE siret = '419750591';

SELECT tva_id, delivery_type, tva_title, tva_rate, show_in_report, enabled
FROM tva_categories ORDER BY tva_id;


-- ---------------------------------------------------------------------
-- 1. CASCADE DU PÉRIMÈTRE — où part le chiffre d'affaires
--    Montre l'effet de chaque filtre du rapport, en euros.
-- ---------------------------------------------------------------------
WITH bornes AS (
  SELECT 'juillet'::text AS mois, '2026-06-30 22:00:00+00'::timestamptz AS d1,
                                  '2026-07-31 22:00:00+00'::timestamptz AS d2
  UNION ALL
  SELECT 'aout', '2026-07-31 22:00:00+00', '2026-08-31 22:00:00+00'
),
base AS (
  SELECT b.mois, o.*
  FROM bornes b
  JOIN orders o ON o.creation_date >= b.d1 AND o.creation_date < b.d2
  WHERE o.merchant_id = '212'
)
SELECT mois, etape, nb, ROUND(eur, 2) AS eur FROM (
  SELECT mois, '1. toutes commandes (tous canaux)' AS etape, 1 AS ord,
         count(*) AS nb, SUM(price)/100.0 AS eur FROM base GROUP BY mois
  UNION ALL
  SELECT mois, '2. brand = WELLO_RESTO', 2, count(*), SUM(price)/100.0
  FROM base WHERE brand='WELLO_RESTO' GROUP BY mois
  UNION ALL
  SELECT mois, '3. + state = CLOSED', 3, count(*), SUM(price)/100.0
  FROM base WHERE brand='WELLO_RESTO' AND state='CLOSED' GROUP BY mois
  UNION ALL
  SELECT mois, '4. + hors DELETED/CANCELED', 4, count(*), SUM(price)/100.0
  FROM base WHERE brand='WELLO_RESTO' AND state='CLOSED'
    AND brand_status NOT IN ('DELETED','CANCELED') GROUP BY mois
  UNION ALL
  SELECT mois, '5. + hors created_by -1/SCANNORDER  <= PERIMETRE RAPPORT', 5,
         count(*), SUM(price)/100.0
  FROM base WHERE brand='WELLO_RESTO' AND state='CLOSED'
    AND brand_status NOT IN ('DELETED','CANCELED')
    AND created_by NOT IN ('-1','SCANNORDER') GROUP BY mois
) x ORDER BY mois DESC, ord;
-- ATTENDU : l'étape 5 doit égaler le total TTC du tableau TVA du PDF.
--   PDF juillet : 60.00 + 771.00 + 6686.20 = 7517.20 EUR
--   PDF août    : 57.00 + 8074.36 + 715.50 = 8846.86 EUR
-- Tout écart entre l'étape 5 et ces totaux = bug (sections 3 et 4 ci-dessous).


-- ---------------------------------------------------------------------
-- 2. REPRODUCTION EXACTE DU TABLEAU TVA DU PDF (requête GetTVAData)
-- ---------------------------------------------------------------------
WITH bornes AS (
  SELECT 'juillet'::text AS mois, '2026-06-30 22:00:00+00'::timestamptz AS d1,
                                  '2026-07-31 22:00:00+00'::timestamptz AS d2
  UNION ALL
  SELECT 'aout', '2026-07-31 22:00:00+00', '2026-08-31 22:00:00+00'
),
src AS (
  SELECT b.mois, tva.tva_title AS titre, tva.tva_rate::numeric AS taux,
         ((oi.price + COALESCE(e.extra_price,0)) * oi.quantity)::numeric AS ttc
  FROM bornes b
  JOIN orders o ON o.creation_date >= b.d1 AND o.creation_date < b.d2
  JOIN orderitems oi ON oi.order_id = o.order_id
  JOIN products p ON p.product_id = oi.product_id
  JOIN tva_categories tva ON tva.tva_id = (CASE
      WHEN o.order_type='DELIVERY'  THEN p.tva_delivery_id
      WHEN o.order_type='TAKE_AWAY' THEN p.tva_take_away_id
      ELSE p.tva_in_id END)
  LEFT JOIN (SELECT order_item_id, SUM(price) AS extra_price
             FROM extra GROUP BY order_item_id) e
         ON e.order_item_id = oi.order_item_id
  WHERE o.merchant_id='212' AND o.state='CLOSED' AND o.brand='WELLO_RESTO'
    AND o.brand_status NOT IN ('DELETED','CANCELED')
    AND o.created_by NOT IN ('-1','SCANNORDER')
    AND tva.show_in_report
  UNION ALL
  SELECT b.mois, tf.tva_title, tf.tva_rate::numeric, o.delivery_fees::numeric
  FROM bornes b
  JOIN orders o ON o.creation_date >= b.d1 AND o.creation_date < b.d2
  JOIN tva_categories tf ON tf.tva_id = -1
  WHERE o.merchant_id='212' AND o.state='CLOSED' AND o.brand='WELLO_RESTO'
    AND o.brand_status NOT IN ('DELETED','CANCELED')
    AND o.created_by NOT IN ('-1','SCANNORDER')
)
SELECT mois, titre, taux,
       ROUND(SUM(ttc)*(100.0/(100.0+taux))/100.0, 2) AS ht_eur,
       ROUND((SUM(ttc) - SUM(ttc)*(100.0/(100.0+taux)))/100.0, 2) AS tva_eur,
       ROUND(SUM(ttc)/100.0, 2) AS ttc_eur
FROM src GROUP BY mois, titre, taux ORDER BY mois DESC, titre;
-- Doit redonner ligne pour ligne le tableau TVA des deux PDF.


-- ---------------------------------------------------------------------
-- 3. CAUSE A — commandes avec des lignes mais un prix incohérent
--    (orders.price <> somme des lignes + frais de livraison)
--    Ces commandes gonflent le tableau TVA sans contrepartie encaissée.
-- ---------------------------------------------------------------------
WITH l AS (
  SELECT o.order_id, o.creation_date, o.order_type, o.created_by, o.brand_status,
         o.price, o.delivery_fees,
         COALESCE(SUM((oi.price + COALESCE(e.extra_price,0)) * oi.quantity), 0) AS items
  FROM orders o
  LEFT JOIN orderitems oi ON oi.order_id = o.order_id
  LEFT JOIN (SELECT order_item_id, SUM(price) AS extra_price
             FROM extra GROUP BY order_item_id) e
         ON e.order_item_id = oi.order_item_id
  WHERE o.merchant_id='212' AND o.brand='WELLO_RESTO' AND o.state='CLOSED'
    AND o.brand_status NOT IN ('DELETED','CANCELED')
    AND o.created_by NOT IN ('-1','SCANNORDER')
    AND o.creation_date >= '2026-06-30 22:00:00+00'
    AND o.creation_date <  '2026-08-31 22:00:00+00'
  GROUP BY 1,2,3,4,5,6,7
)
SELECT order_id, creation_date AT TIME ZONE 'Europe/Paris' AS date_locale,
       order_type, created_by, brand_status,
       price/100.0 AS orders_price_eur, items/100.0 AS lignes_eur,
       delivery_fees/100.0 AS frais_eur,
       (items + delivery_fees - price)/100.0 AS ecart_eur
FROM l
WHERE price <> items + delivery_fees
ORDER BY abs(items + delivery_fees - price) DESC;
-- La somme de la colonne ecart_eur explique l'écart entre le tableau TVA
-- et les encaissements.


-- ---------------------------------------------------------------------
-- 4. CAUSE B — chiffre d'affaires silencieusement ABSENT du tableau TVA
--    4a : produits rattachés à une catégorie show_in_report = false
--         (typiquement tva_id = 0 « TVA Undefined »)
--    4b : produits dont le tva_id n'existe pas dans tva_categories
--         (INNER JOIN => la ligne disparaît, sans aucune alerte)
-- ---------------------------------------------------------------------
-- 4a
SELECT to_char(o.creation_date AT TIME ZONE 'Europe/Paris','YYYY-MM') AS mois,
       tva.tva_id, tva.tva_title, tva.show_in_report,
       count(*) AS nb_lignes,
       ROUND(SUM((oi.price + COALESCE(e.extra_price,0)) * oi.quantity)/100.0, 2) AS ttc_eur
FROM orders o
JOIN orderitems oi ON oi.order_id = o.order_id
JOIN products p ON p.product_id = oi.product_id
JOIN tva_categories tva ON tva.tva_id = (CASE
    WHEN o.order_type='DELIVERY'  THEN p.tva_delivery_id
    WHEN o.order_type='TAKE_AWAY' THEN p.tva_take_away_id
    ELSE p.tva_in_id END)
LEFT JOIN (SELECT order_item_id, SUM(price) AS extra_price
           FROM extra GROUP BY order_item_id) e
       ON e.order_item_id = oi.order_item_id
WHERE o.merchant_id='212' AND o.brand='WELLO_RESTO' AND o.state='CLOSED'
  AND o.brand_status NOT IN ('DELETED','CANCELED')
  AND o.created_by NOT IN ('-1','SCANNORDER')
  AND o.creation_date >= '2026-06-30 22:00:00+00'
  AND o.creation_date <  '2026-08-31 22:00:00+00'
  AND tva.show_in_report = false
GROUP BY 1,2,3,4 ORDER BY 1,6 DESC;

-- 4b
SELECT to_char(o.creation_date AT TIME ZONE 'Europe/Paris','YYYY-MM') AS mois,
       p.product_id, p.name, o.order_type,
       p.tva_in_id, p.tva_take_away_id, p.tva_delivery_id,
       count(*) AS nb_lignes,
       ROUND(SUM(oi.price * oi.quantity)/100.0, 2) AS ttc_eur_perdu
FROM orders o
JOIN orderitems oi ON oi.order_id = o.order_id
JOIN products p ON p.product_id = oi.product_id
LEFT JOIN tva_categories tva ON tva.tva_id = (CASE
    WHEN o.order_type='DELIVERY'  THEN p.tva_delivery_id
    WHEN o.order_type='TAKE_AWAY' THEN p.tva_take_away_id
    ELSE p.tva_in_id END)
WHERE o.merchant_id='212' AND o.brand='WELLO_RESTO' AND o.state='CLOSED'
  AND o.brand_status NOT IN ('DELETED','CANCELED')
  AND o.created_by NOT IN ('-1','SCANNORDER')
  AND o.creation_date >= '2026-06-30 22:00:00+00'
  AND o.creation_date <  '2026-08-31 22:00:00+00'
  AND tva.tva_id IS NULL
GROUP BY 1,2,3,4,5,6,7 ORDER BY 1,9 DESC;


-- ---------------------------------------------------------------------
-- 5. CAUSE C — mauvais taux de TVA appliqué
--    5a : order_type NULL => le code retombe sur le taux « sur place »
--    5b : frais de livraison facturés sur des commandes non-DELIVERY
-- ---------------------------------------------------------------------
-- 5a
SELECT to_char(creation_date AT TIME ZONE 'Europe/Paris','YYYY-MM') AS mois,
       COALESCE(order_type,'(NULL)') AS order_type,
       count(*) AS nb, ROUND(SUM(price)/100.0, 2) AS eur
FROM orders
WHERE merchant_id='212' AND brand='WELLO_RESTO' AND state='CLOSED'
  AND brand_status NOT IN ('DELETED','CANCELED')
  AND created_by NOT IN ('-1','SCANNORDER')
  AND creation_date >= '2026-06-30 22:00:00+00'
  AND creation_date <  '2026-08-31 22:00:00+00'
GROUP BY 1,2 ORDER BY 1,4 DESC;

-- 5b
SELECT to_char(creation_date AT TIME ZONE 'Europe/Paris','YYYY-MM') AS mois,
       COALESCE(order_type,'(NULL)') AS order_type,
       count(*) AS nb, ROUND(SUM(delivery_fees)/100.0, 2) AS frais_eur
FROM orders
WHERE merchant_id='212' AND brand='WELLO_RESTO' AND state='CLOSED'
  AND brand_status NOT IN ('DELETED','CANCELED')
  AND created_by NOT IN ('-1','SCANNORDER')
  AND delivery_fees <> 0
  AND creation_date >= '2026-06-30 22:00:00+00'
  AND creation_date <  '2026-08-31 22:00:00+00'
GROUP BY 1,2 ORDER BY 1,2;
-- Toute ligne order_type <> 'DELIVERY' ici = frais de port taxés à 20 %
-- sur une commande qui n'est pas une livraison.


-- ---------------------------------------------------------------------
-- 6. CAUSE D — commandes encaissées mais hors périmètre brand_status
--    Le code n'exclut que DELETED et CANCELED : DENIED, PENDING,
--    ONLINE_PAYMENT_PENDING... sont comptés comme du CA.
-- ---------------------------------------------------------------------
SELECT to_char(creation_date AT TIME ZONE 'Europe/Paris','YYYY-MM') AS mois,
       brand_status, count(*) AS nb, ROUND(SUM(price)/100.0, 2) AS eur
FROM orders
WHERE merchant_id='212' AND brand='WELLO_RESTO' AND state='CLOSED'
  AND brand_status NOT IN ('DELETED','CANCELED')
  AND created_by NOT IN ('-1','SCANNORDER')
  AND creation_date >= '2026-06-30 22:00:00+00'
  AND creation_date <  '2026-08-31 22:00:00+00'
GROUP BY 1,2 ORDER BY 1,4 DESC;
-- Toute valeur autre que CLOSED / DONE mérite un arbitrage comptable.


-- ---------------------------------------------------------------------
-- 7. SECTION ENCAISSEMENTS — les deux sources possibles, comparées
--    7a : le « réel » que le code utilise réellement
--         (cash_registers_custom_items des registres enclosed du mois)
--    7b : le « théorique » (table payments)
--    7c : registres du mois et registres ÉCARTÉS par le contrôle de dérive
-- ---------------------------------------------------------------------
-- 7a
SELECT to_char(cr.start_date AT TIME ZONE 'Europe/Paris','YYYY-MM') AS mois,
       COALESCE(NULLIF(l.label,''), crci.label) AS libelle,
       ROUND(SUM(crci.amount)/100.0, 2) AS eur
FROM cash_registers_custom_items crci
JOIN cash_registers cr ON cr.cash_register_id = crci.cash_register_id
LEFT JOIN labels l ON l.label_type='mop' AND l.label_value=crci.label AND l.lang='FR'
WHERE cr.merchant_id='212' AND cr.enclosed = TRUE AND crci.enabled = TRUE
  AND crci.label NOT IN ('STRIPE','UBER_EATS','DELIVEROO')
  AND cr.start_date >= '2026-06-30 22:00:00+00'
  AND cr.start_date <  '2026-08-31 22:00:00+00'
GROUP BY 1,2 ORDER BY 1,2;

-- 7b
SELECT to_char(o.creation_date AT TIME ZONE 'Europe/Paris','YYYY-MM') AS mois,
       l.label AS libelle, ROUND(SUM(p.amount)/100.0, 2) AS eur
FROM payments p
JOIN orders o ON o.order_id = p.order_id
JOIN labels l ON l.label_type='mop' AND l.label_value=p.mop AND l.lang='FR'
WHERE p.merchant_id='212' AND p.enabled = TRUE
  AND o.state='CLOSED' AND o.brand='WELLO_RESTO'
  AND o.brand_status NOT IN ('DELETED','CANCELED')
  AND o.created_by NOT IN ('-1','SCANNORDER')
  AND o.creation_date >= '2026-06-30 22:00:00+00'
  AND o.creation_date <  '2026-08-31 22:00:00+00'
GROUP BY 1,2 ORDER BY 1,2;
-- Comparer 7a et 7b aux lignes « Encaissements » du PDF pour savoir
-- laquelle des deux sources a produit les chiffres imprimés.

-- 7c : registres écartés par GetTrustedEnclosedRegisterIDs
--      (snapshot figé cash_registers_items != recalcul live des paiements)
WITH reg AS (
  SELECT cash_register_id FROM cash_registers
  WHERE merchant_id='212' AND enclosed = TRUE
    AND start_date >= '2026-06-30 22:00:00+00'
    AND start_date <  '2026-08-31 22:00:00+00'
),
fige AS (
  SELECT cri.cash_register_id, cri.mop, SUM(cri.amount) AS montant
  FROM cash_registers_items cri JOIN reg ON reg.cash_register_id = cri.cash_register_id
  GROUP BY 1,2
),
live AS (
  SELECT p.cash_register_id::int AS cash_register_id, p.mop, SUM(p.amount) AS montant
  FROM payments p
  JOIN orders o ON o.order_id = p.order_id
  JOIN reg ON reg.cash_register_id::text = p.cash_register_id
  WHERE o.brand_status NOT IN ('DELETED','CANCELED') AND p.enabled IS TRUE
  GROUP BY 1,2
)
SELECT COALESCE(f.cash_register_id, v.cash_register_id) AS cash_register_id,
       COALESCE(f.mop, v.mop) AS mop,
       COALESCE(f.montant,0)/100.0 AS fige_eur,
       COALESCE(v.montant,0)/100.0 AS live_eur,
       (COALESCE(v.montant,0) - COALESCE(f.montant,0))/100.0 AS derive_eur
FROM fige f
FULL OUTER JOIN live v
  ON v.cash_register_id = f.cash_register_id AND v.mop = f.mop
WHERE COALESCE(f.montant,-1) <> COALESCE(v.montant,-1)
ORDER BY 1,2;
-- CHAQUE registre listé ici est ENTIÈREMENT exclu des encaissements du PDF,
-- sans aucune mention sur le document (seulement un log WARN serveur).


-- ---------------------------------------------------------------------
-- 8. CAUSE E — extras mal comptés
--    8a : extras avec quantity > 1 (le code somme price en ignorant quantity)
--    8b : extras dont order_item_id est NULL (jamais rattachés => perdus)
-- ---------------------------------------------------------------------
SELECT to_char(o.creation_date AT TIME ZONE 'Europe/Paris','YYYY-MM') AS mois,
       count(*) FILTER (WHERE ex.quantity > 1) AS nb_qty_sup_1,
       ROUND(COALESCE(SUM(ex.price*(ex.quantity-1)) FILTER (WHERE ex.quantity > 1),0)/100.0, 2)
         AS eur_non_compte,
       count(*) FILTER (WHERE ex.order_item_id IS NULL) AS nb_item_null,
       ROUND(COALESCE(SUM(ex.price) FILTER (WHERE ex.order_item_id IS NULL),0)/100.0, 2)
         AS eur_perdu
FROM extra ex
JOIN orders o ON o.order_id = ex.order_id
WHERE o.merchant_id='212' AND o.brand='WELLO_RESTO' AND o.state='CLOSED'
  AND o.creation_date >= '2026-06-30 22:00:00+00'
  AND o.creation_date <  '2026-08-31 22:00:00+00'
GROUP BY 1 ORDER BY 1;


-- ---------------------------------------------------------------------
-- 9. CAUSE F — décalage de fuseau entre le PDF et l'écran TVA du back-office
--    Le PDF borne en heure de Paris ; l'endpoint /accounting/vat borne en UTC.
--    Les commandes ci-dessous basculent d'un mois à l'autre selon la source.
-- ---------------------------------------------------------------------
SELECT order_id, creation_date AT TIME ZONE 'Europe/Paris' AS date_paris,
       creation_date AT TIME ZONE 'UTC' AS date_utc,
       order_type, ROUND(price/100.0, 2) AS eur
FROM orders
WHERE merchant_id='212' AND brand='WELLO_RESTO' AND state='CLOSED'
  AND brand_status NOT IN ('DELETED','CANCELED')
  AND created_by NOT IN ('-1','SCANNORDER')
  AND (
       (creation_date >= '2026-06-30 22:00:00+00' AND creation_date < '2026-07-01 00:00:00+00')
    OR (creation_date >= '2026-07-31 22:00:00+00' AND creation_date < '2026-08-01 00:00:00+00')
    OR (creation_date >= '2026-08-31 22:00:00+00' AND creation_date < '2026-09-01 00:00:00+00')
  )
ORDER BY creation_date;

ROLLBACK;
