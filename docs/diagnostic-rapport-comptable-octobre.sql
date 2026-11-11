-- =====================================================================
-- DIAGNOSTIC - Rapport comptable Croq'O'Pizzas (merchant 212)
-- Octobre 2026 - 100 % READ-ONLY
--
-- PDF a expliquer (WR_rapport_comptable_20261001_20261031_20261110T133743Z) :
--   TVA TTC       : 9298.52 + 445.70 + 75.00  = 9819.22 EUR
--   Encaissements : 1242.47 + 8296.05 + 47.40 = 9585.92 EUR
--   Ecart         : 233.30 EUR de TVA sans encaissement correspondant
--
-- Ce script remplace les versions juillet-septembre (diagnostic-rapport-
-- comptable-croq-o-pizzas.sql), qui decrivent l'ancien code. Filtres alignes
-- sur internal/modules/pos/accounting/repository.go :
--   GetOrderVATLines, GetOrderPayments, GetTrustedEnclosedRegisterIDs,
--   GetRealPaymentsData, et models.VoidOrderBrandStatusesSQL (inclut DENIED).
--
-- Bornes : le PDF travaille en heure locale Europe/Paris. Passage a l'heure
-- d'hiver le 25/10/2026, donc octobre = [2026-09-30 22:00Z , 2026-10-31 23:00Z[.
--
-- Executer section par section. Aucune requete ne contient de point-virgule
-- hors fin d'instruction.
-- =====================================================================

BEGIN;
SET TRANSACTION READ ONLY;

-- ---------------------------------------------------------------------
-- 0. QUEL MODE DE CLOTURE A PRODUIT CE PDF ?  (a lire en premier)
--    AUTO   : le total TVA = le total des encaissements PAR CONSTRUCTION
--             (autoNetShares). Un ecart en AUTO est donc un bug de code.
--    MANUAL : la TVA vient des lignes vendues, les encaissements du reel saisi
--             sur les registres. Un ecart est possible et se cherche en 3 a 7.
-- ---------------------------------------------------------------------
SHOW timezone;
-- Les bornes du code sont des textes 'YYYY-MM-DD HH:MM:SS' sans fuseau : la
-- base les lit dans le fuseau de SA session. Si ce n'est pas UTC, le PDF a ete
-- calcule sur une periode decalee et ce script ne le reproduira pas a l'identique.

SELECT mode, effective_from FROM merchant_closing_modes
WHERE merchant_id = '212' ORDER BY effective_from;
-- Le mode du rapport est celui de la derniere ligne dont effective_from <= 2026-10-01.
-- Un changement de mode DANS octobre est refuse par l'export, donc impossible ici.

SELECT id, fullname, siret, timezone FROM merchant WHERE id = 212;

SELECT id, period_from, period_to, closing_mode, channels, filename, size_bytes, generated_by, generated_at
FROM accounting_exports WHERE merchant_id = '212' ORDER BY id DESC LIMIT 10;

SELECT tva_id, delivery_type, tva_title, tva_rate, show_in_report, enabled
FROM tva_categories ORDER BY tva_id;


-- ---------------------------------------------------------------------
-- 1. CASCADE DU PERIMETRE - ou part le chiffre d'affaires (en euros)
--    Etape 5 = perimetre MANUAL, etape 6 = perimetre AUTO tous canaux.
-- ---------------------------------------------------------------------
WITH base AS (
  SELECT o.* FROM orders o
  WHERE o.merchant_id = '212'
    AND o.creation_date >= '2026-09-30 22:00:00+00'
    AND o.creation_date <  '2026-10-31 23:00:00+00'
)
SELECT etape, nb, ROUND(eur, 2) AS eur FROM (
  SELECT '1. toutes commandes, tous etats' AS etape, 1 AS ord, count(*) AS nb, SUM(price)/100.0 AS eur FROM base
  UNION ALL
  SELECT '2. + state = CLOSED', 2, count(*), SUM(price)/100.0 FROM base WHERE state = 'CLOSED'
  UNION ALL
  SELECT '3. + hors DELETED/CANCELED/DENIED/DELIVERY_CANCELED/DELIVERY_FAILED', 3, count(*), SUM(price)/100.0
  FROM base WHERE state = 'CLOSED'
    AND upper(brand_status) NOT IN ('DELETED','CANCELED','DENIED','DELIVERY_CANCELED','DELIVERY_FAILED')
  UNION ALL
  SELECT '4. = perimetre AUTO (tous canaux)', 4, count(*), SUM(price)/100.0
  FROM base WHERE state = 'CLOSED'
    AND upper(brand_status) NOT IN ('DELETED','CANCELED','DENIED','DELIVERY_CANCELED','DELIVERY_FAILED')
  UNION ALL
  SELECT '5. = perimetre MANUAL (brand WELLO_RESTO, hors -1/SCANNORDER)', 5, count(*), SUM(price)/100.0
  FROM base WHERE state = 'CLOSED'
    AND upper(brand_status) NOT IN ('DELETED','CANCELED','DENIED','DELIVERY_CANCELED','DELIVERY_FAILED')
    AND brand = 'WELLO_RESTO' AND created_by NOT IN ('-1','SCANNORDER')
) x ORDER BY ord;

-- Repartition par canal et par marque des commandes retenues
SELECT COALESCE(order_source,'(NULL)') AS order_source, brand, count(*) AS nb, ROUND(SUM(price)/100.0, 2) AS eur
FROM orders
WHERE merchant_id = '212' AND state = 'CLOSED'
  AND upper(brand_status) NOT IN ('DELETED','CANCELED','DENIED','DELIVERY_CANCELED','DELIVERY_FAILED')
  AND creation_date >= '2026-09-30 22:00:00+00' AND creation_date < '2026-10-31 23:00:00+00'
GROUP BY 1,2 ORDER BY 4 DESC;


-- ---------------------------------------------------------------------
-- 2. TABLEAU TVA - reproduction de GetOrderVATLines (perimetre MANUAL)
--    TVA figee sur la ligne (orderitems.tva_id / tva_rate) sinon celle du
--    produit, options payantes incluses (hors Uber Eats / Deliveroo), frais de
--    livraison a part. Ce sont les montants AVANT deduction des remises.
-- ---------------------------------------------------------------------
WITH src AS (
  SELECT tva.tva_title AS titre, COALESCE(oi.tva_rate, tva.tva_rate)::numeric AS taux, tva.show_in_report AS affiche,
         ((oi.price + COALESCE(e.extra_price,0)
           + CASE WHEN COALESCE(o.brand,'') IN ('UBER_EATS','DELIVEROO') THEN 0 ELSE COALESCE(opt.option_price,0) END)
          * oi.quantity)::numeric AS ttc
  FROM orders o
  JOIN orderitems oi ON oi.order_id = o.order_id
  JOIN products p ON p.product_id = oi.product_id
  JOIN tva_categories tva ON tva.tva_id = COALESCE(oi.tva_id, CASE
      WHEN o.order_type = 'DELIVERY'  THEN p.tva_delivery_id
      WHEN o.order_type = 'TAKE_AWAY' THEN p.tva_take_away_id
      ELSE p.tva_in_id END)
  LEFT JOIN (SELECT order_item_id, SUM(price) AS extra_price FROM extra GROUP BY order_item_id) e
         ON e.order_item_id = oi.order_item_id
  LEFT JOIN (SELECT oic.order_item_id,
                    CAST(SUM(oic.quantity * COALESCE(oic.extra_price, cao.extra_price, 0)) AS bigint) AS option_price
             FROM order_item_configuration oic
             LEFT JOIN configurable_attribute_options cao ON cao.id = oic.configuration_attribute_option_id
             WHERE oic.quantity > 0 GROUP BY oic.order_item_id) opt
         ON opt.order_item_id = oi.order_item_id
  WHERE o.merchant_id = '212' AND o.state = 'CLOSED'
    AND upper(o.brand_status) NOT IN ('DELETED','CANCELED','DENIED','DELIVERY_CANCELED','DELIVERY_FAILED')
    AND o.brand = 'WELLO_RESTO' AND o.created_by NOT IN ('-1','SCANNORDER')
    AND o.creation_date >= '2026-09-30 22:00:00+00' AND o.creation_date < '2026-10-31 23:00:00+00'
  UNION ALL
  SELECT tf.tva_title, COALESCE(o.delivery_fees_tva_rate, tf.tva_rate)::numeric, TRUE, o.delivery_fees::numeric
  FROM orders o
  JOIN tva_categories tf ON tf.tva_id = -1
  WHERE o.merchant_id = '212' AND o.state = 'CLOSED' AND o.delivery_fees <> 0
    AND upper(o.brand_status) NOT IN ('DELETED','CANCELED','DENIED','DELIVERY_CANCELED','DELIVERY_FAILED')
    AND o.brand = 'WELLO_RESTO' AND o.created_by NOT IN ('-1','SCANNORDER')
    AND o.creation_date >= '2026-09-30 22:00:00+00' AND o.creation_date < '2026-10-31 23:00:00+00'
)
SELECT titre, taux, affiche,
       ROUND(SUM(ttc)*(100.0/(100.0+taux))/100.0, 2) AS ht_eur,
       ROUND((SUM(ttc) - SUM(ttc)*(100.0/(100.0+taux)))/100.0, 2) AS tva_eur,
       ROUND(SUM(ttc)/100.0, 2) AS ttc_eur
FROM src GROUP BY titre, taux, affiche ORDER BY titre;
-- Rapprocher des 3 lignes du PDF : 10 % = 9298.52 / 5.5 % = 445.70 / livraison = 75.00.
-- Le PDF MANUAL deduit en plus les remises de caisse (section 2b) : si le
-- total ci-dessus est plus grand que 9819.22, la difference = remises.
-- Les lignes affiche = false n'apparaissent pas sur le PDF en mode MANUAL.

-- 2b. Remises de caisse (CURRENCY / PERCENTAGE / DISCOUNT) du perimetre, retirees de la TVA en MANUAL
SELECT p.mop, count(*) AS nb, ROUND(SUM(p.amount)/100.0, 2) AS eur
FROM payments p JOIN orders o ON o.order_id = p.order_id
WHERE o.merchant_id = '212' AND o.state = 'CLOSED' AND p.enabled = TRUE
  AND upper(o.brand_status) NOT IN ('DELETED','CANCELED','DENIED','DELIVERY_CANCELED','DELIVERY_FAILED')
  AND o.brand = 'WELLO_RESTO' AND o.created_by NOT IN ('-1','SCANNORDER')
  AND o.creation_date >= '2026-09-30 22:00:00+00' AND o.creation_date < '2026-10-31 23:00:00+00'
  AND upper(trim(p.mop)) IN ('CURRENCY','PERCENTAGE','DISCOUNT')
GROUP BY 1;


-- ---------------------------------------------------------------------
-- 3. ENCAISSEMENTS EN MODE MANUAL - reproduction de GetRealPaymentsData
--    3a : exactement ce que lit le PDF (registres de confiance uniquement)
--    3b : registres ECARTES par le controle de derive (absents du PDF sans mention)
--    3c : reel saisi vs theorique fige, registre par registre
-- ---------------------------------------------------------------------
-- 3a
WITH reg AS (
  SELECT cash_register_id FROM cash_registers
  WHERE merchant_id = '212' AND enclosed = TRUE
    AND start_date >= '2026-09-30 22:00:00+00' AND start_date < '2026-10-31 23:00:00+00'
),
fige AS (
  SELECT cri.cash_register_id, cri.mop, SUM(cri.amount) AS montant
  FROM cash_registers_items cri JOIN reg USING (cash_register_id) GROUP BY 1,2
),
live AS (
  SELECT p.cash_register_id::int AS cash_register_id, p.mop, SUM(p.amount) AS montant
  FROM payments p JOIN orders o ON o.order_id = p.order_id
  JOIN reg ON reg.cash_register_id::text = p.cash_register_id
  WHERE o.brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED') AND p.enabled IS TRUE
  GROUP BY 1,2
),
derive AS (
  SELECT DISTINCT COALESCE(f.cash_register_id, v.cash_register_id) AS cash_register_id
  FROM fige f FULL OUTER JOIN live v ON v.cash_register_id = f.cash_register_id AND v.mop = f.mop
  WHERE COALESCE(f.montant,-1) <> COALESCE(v.montant,-1)
),
reel AS (
  SELECT COALESCE(NULLIF(l.label,''), crci.label) AS libelle, crci.amount
  FROM cash_registers_custom_items crci JOIN reg USING (cash_register_id)
  LEFT JOIN labels l ON l.label_type = 'mop' AND l.label_value = crci.label AND l.lang = 'FR'
  WHERE crci.enabled = TRUE AND crci.label NOT IN ('STRIPE','UBER_EATS','DELIVEROO')
    AND crci.cash_register_id NOT IN (SELECT cash_register_id FROM derive)
)
SELECT libelle, ROUND(SUM(amount)/100.0, 2) AS eur
FROM reel
WHERE upper(trim(libelle)) NOT IN ('UBER EATS','DELIVEROO','SCANNORDER')
GROUP BY libelle HAVING SUM(amount) <> 0 ORDER BY libelle;
-- ATTENDU (PDF) : Borne de commande 1242.47 / Carte bancaire 8296.05 / Espece 47.40

-- 3b : registres ecartes (instantane fige <> recalcul live)
WITH reg AS (
  SELECT cash_register_id FROM cash_registers
  WHERE merchant_id = '212' AND enclosed = TRUE
    AND start_date >= '2026-09-30 22:00:00+00' AND start_date < '2026-10-31 23:00:00+00'
),
fige AS (
  SELECT cri.cash_register_id, cri.mop, SUM(cri.amount) AS montant
  FROM cash_registers_items cri JOIN reg USING (cash_register_id) GROUP BY 1,2
),
live AS (
  SELECT p.cash_register_id::int AS cash_register_id, p.mop, SUM(p.amount) AS montant
  FROM payments p JOIN orders o ON o.order_id = p.order_id
  JOIN reg ON reg.cash_register_id::text = p.cash_register_id
  WHERE o.brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED') AND p.enabled IS TRUE
  GROUP BY 1,2
)
SELECT COALESCE(f.cash_register_id, v.cash_register_id) AS cash_register_id, COALESCE(f.mop, v.mop) AS mop,
       COALESCE(f.montant,0)/100.0 AS fige_eur, COALESCE(v.montant,0)/100.0 AS live_eur,
       (COALESCE(v.montant,0) - COALESCE(f.montant,0))/100.0 AS derive_eur
FROM fige f FULL OUTER JOIN live v ON v.cash_register_id = f.cash_register_id AND v.mop = f.mop
WHERE COALESCE(f.montant,-1) <> COALESCE(v.montant,-1)
ORDER BY 1,2;

-- 3c : reel saisi (ce que lit le PDF) contre theorique fige, par registre
WITH reg AS (
  SELECT cash_register_id, start_date, end_date FROM cash_registers
  WHERE merchant_id = '212' AND enclosed = TRUE
    AND start_date >= '2026-09-30 22:00:00+00' AND start_date < '2026-10-31 23:00:00+00'
),
reel AS (
  SELECT crci.cash_register_id, SUM(crci.amount) AS montant
  FROM cash_registers_custom_items crci JOIN reg USING (cash_register_id)
  WHERE crci.enabled = TRUE AND crci.label NOT IN ('STRIPE','UBER_EATS','DELIVEROO') GROUP BY 1
),
fige AS (
  SELECT cri.cash_register_id, SUM(cri.amount) AS montant
  FROM cash_registers_items cri JOIN reg USING (cash_register_id)
  WHERE cri.mop NOT IN ('STRIPE','UBER_EATS','DELIVEROO') GROUP BY 1
)
SELECT reg.cash_register_id,
       reg.start_date AT TIME ZONE 'Europe/Paris' AS ouverture_locale,
       reg.end_date   AT TIME ZONE 'Europe/Paris' AS fermeture_locale,
       COALESCE(reel.montant,0)/100.0 AS reel_eur, COALESCE(fige.montant,0)/100.0 AS theorique_fige_eur,
       (COALESCE(reel.montant,0) - COALESCE(fige.montant,0))/100.0 AS ecart_eur
FROM reg LEFT JOIN reel USING (cash_register_id) LEFT JOIN fige USING (cash_register_id)
ORDER BY abs(COALESCE(reel.montant,0) - COALESCE(fige.montant,0)) DESC;


-- ---------------------------------------------------------------------
-- 4. RAPPROCHEMENT COMMANDE PAR COMMANDE (valable dans les deux modes)
--    Pour chaque commande du perimetre : TTC des lignes + frais, remises,
--    paiements reels. Ne restent que les commandes dont TTC - remises <> paiements.
--    ecart_eur > 0 : vendu mais pas (assez) encaisse. ecart_eur < 0 : encaisse en trop.
--    La somme de ecart_eur ~ l'ecart du PDF (233.30 EUR) en mode AUTO ou si les
--    registres couvrent exactement les commandes du mois.
-- ---------------------------------------------------------------------
WITH ord AS (
  SELECT o.order_id, o.creation_date, o.order_type, o.order_source, o.brand, o.created_by, o.cash_register_id, o.price, o.delivery_fees
  FROM orders o
  WHERE o.merchant_id = '212' AND o.state = 'CLOSED'
    AND upper(o.brand_status) NOT IN ('DELETED','CANCELED','DENIED','DELIVERY_CANCELED','DELIVERY_FAILED')
    AND o.brand = 'WELLO_RESTO' AND o.created_by NOT IN ('-1','SCANNORDER')   -- perimetre MANUAL. Pour AUTO, retirer ces 2 criteres
    AND o.creation_date >= '2026-09-30 22:00:00+00' AND o.creation_date < '2026-10-31 23:00:00+00'
),
lignes AS (
  SELECT oi.order_id,
         SUM((oi.price + COALESCE(e.extra_price,0)
              + CASE WHEN o.brand IN ('UBER_EATS','DELIVEROO') THEN 0 ELSE COALESCE(opt.option_price,0) END) * oi.quantity) AS ttc
  FROM ord o
  JOIN orderitems oi ON oi.order_id = o.order_id
  JOIN products p ON p.product_id = oi.product_id
  JOIN tva_categories tva ON tva.tva_id = COALESCE(oi.tva_id, CASE
      WHEN o.order_type = 'DELIVERY' THEN p.tva_delivery_id WHEN o.order_type = 'TAKE_AWAY' THEN p.tva_take_away_id ELSE p.tva_in_id END)
  LEFT JOIN (SELECT order_item_id, SUM(price) AS extra_price FROM extra GROUP BY order_item_id) e ON e.order_item_id = oi.order_item_id
  LEFT JOIN (SELECT oic.order_item_id,
                    CAST(SUM(oic.quantity * COALESCE(oic.extra_price, cao.extra_price, 0)) AS bigint) AS option_price
             FROM order_item_configuration oic
             LEFT JOIN configurable_attribute_options cao ON cao.id = oic.configuration_attribute_option_id
             WHERE oic.quantity > 0 GROUP BY oic.order_item_id) opt ON opt.order_item_id = oi.order_item_id
  GROUP BY oi.order_id
),
paiements AS (
  SELECT p.order_id,
         SUM(p.amount) FILTER (WHERE upper(trim(p.mop)) NOT IN ('CURRENCY','PERCENTAGE','DISCOUNT')) AS reel,
         SUM(p.amount) FILTER (WHERE upper(trim(p.mop))     IN ('CURRENCY','PERCENTAGE','DISCOUNT')) AS remise
  FROM payments p WHERE p.enabled = TRUE GROUP BY p.order_id
)
SELECT o.order_id, o.creation_date AT TIME ZONE 'Europe/Paris' AS date_locale,
       o.order_type, o.order_source, o.cash_register_id,
       o.price/100.0 AS orders_price_eur,
       (COALESCE(l.ttc,0) + o.delivery_fees)/100.0 AS tva_ttc_eur,
       COALESCE(pa.remise,0)/100.0 AS remises_eur,
       COALESCE(pa.reel,0)/100.0 AS encaisse_eur,
       (COALESCE(l.ttc,0) + o.delivery_fees - COALESCE(pa.remise,0) - COALESCE(pa.reel,0))/100.0 AS ecart_eur
FROM ord o
LEFT JOIN lignes l ON l.order_id = o.order_id
LEFT JOIN paiements pa ON pa.order_id = o.order_id
WHERE COALESCE(l.ttc,0) + o.delivery_fees - COALESCE(pa.remise,0) - COALESCE(pa.reel,0) <> 0
ORDER BY abs(COALESCE(l.ttc,0) + o.delivery_fees - COALESCE(pa.remise,0) - COALESCE(pa.reel,0)) DESC
LIMIT 100;
-- Lire la colonne encaisse_eur : 0 = commande jamais payee (ou paiement desactive,
-- enabled = false). orders_price_eur <> tva_ttc_eur = ligne/option/extra perdu (section 6).


-- ---------------------------------------------------------------------
-- 5. CAUSE : paiements sur registres a cheval sur deux mois
--    La TVA prend les commandes CREEES en octobre, les encaissements MANUAL
--    prennent les registres OUVERTS en octobre.
--    5a : paiements des registres d'octobre sur des commandes d'un autre mois
--    5b : paiements des commandes d'octobre selon le registre qui les porte
-- ---------------------------------------------------------------------
-- 5a
SELECT to_char(o.creation_date AT TIME ZONE 'Europe/Paris','YYYY-MM-DD') AS jour_commande,
       p.cash_register_id, count(DISTINCT o.order_id) AS nb_commandes, ROUND(SUM(p.amount)/100.0, 2) AS eur
FROM payments p
JOIN orders o ON o.order_id = p.order_id
JOIN cash_registers cr ON cr.cash_register_id::text = p.cash_register_id
WHERE cr.merchant_id = '212' AND cr.enclosed = TRUE
  AND cr.start_date >= '2026-09-30 22:00:00+00' AND cr.start_date < '2026-10-31 23:00:00+00'
  AND p.enabled IS TRUE
  AND (o.creation_date < '2026-09-30 22:00:00+00' OR o.creation_date >= '2026-10-31 23:00:00+00')
GROUP BY 1,2 ORDER BY 1,2;

-- 5b
SELECT CASE
         WHEN cr.cash_register_id IS NULL THEN 'aucun registre (' || COALESCE(p.cash_register_id,'NULL') || ')'
         WHEN NOT cr.enclosed THEN 'registre non valide'
         WHEN cr.start_date < '2026-09-30 22:00:00+00' OR cr.start_date >= '2026-10-31 23:00:00+00' THEN 'registre ouvert hors octobre'
         ELSE 'registre d''octobre'
       END AS rattachement,
       p.mop, count(*) AS nb, ROUND(SUM(p.amount)/100.0, 2) AS eur
FROM payments p
JOIN orders o ON o.order_id = p.order_id
LEFT JOIN cash_registers cr ON cr.cash_register_id::text = p.cash_register_id
WHERE o.merchant_id = '212' AND o.state = 'CLOSED' AND o.brand = 'WELLO_RESTO'
  AND upper(o.brand_status) NOT IN ('DELETED','CANCELED','DENIED','DELIVERY_CANCELED','DELIVERY_FAILED')
  AND o.created_by NOT IN ('-1','SCANNORDER')
  AND o.creation_date >= '2026-09-30 22:00:00+00' AND o.creation_date < '2026-10-31 23:00:00+00'
  AND p.enabled IS TRUE
GROUP BY 1,2 ORDER BY 1,2;
-- Les lignes autres que 'registre d'octobre' sont des ventes de la TVA absentes
-- des encaissements MANUAL du PDF.


-- ---------------------------------------------------------------------
-- 6. CAUSE : commandes dont le prix ne correspond pas a leurs lignes
--    (orders.price <> lignes + extras + options payantes + frais de livraison)
-- ---------------------------------------------------------------------
WITH l AS (
  SELECT o.order_id, o.creation_date, o.order_type, o.created_by, o.brand_status, o.price, o.delivery_fees,
         COALESCE(SUM((oi.price + COALESCE(e.extra_price,0) + COALESCE(opt.option_price,0)) * oi.quantity), 0) AS items
  FROM orders o
  LEFT JOIN orderitems oi ON oi.order_id = o.order_id
  LEFT JOIN (SELECT order_item_id, SUM(price) AS extra_price FROM extra GROUP BY order_item_id) e ON e.order_item_id = oi.order_item_id
  LEFT JOIN (SELECT oic.order_item_id,
                    CAST(SUM(oic.quantity * COALESCE(oic.extra_price, cao.extra_price, 0)) AS bigint) AS option_price
             FROM order_item_configuration oic
             LEFT JOIN configurable_attribute_options cao ON cao.id = oic.configuration_attribute_option_id
             WHERE oic.quantity > 0 GROUP BY oic.order_item_id) opt ON opt.order_item_id = oi.order_item_id
  WHERE o.merchant_id = '212' AND o.brand = 'WELLO_RESTO' AND o.state = 'CLOSED'
    AND upper(o.brand_status) NOT IN ('DELETED','CANCELED','DENIED','DELIVERY_CANCELED','DELIVERY_FAILED')
    AND o.created_by NOT IN ('-1','SCANNORDER')
    AND o.creation_date >= '2026-09-30 22:00:00+00' AND o.creation_date < '2026-10-31 23:00:00+00'
  GROUP BY 1,2,3,4,5,6,7
)
SELECT order_id, creation_date AT TIME ZONE 'Europe/Paris' AS date_locale, order_type, created_by, brand_status,
       price/100.0 AS orders_price_eur, items/100.0 AS lignes_eur, delivery_fees/100.0 AS frais_eur,
       (items + delivery_fees - price)/100.0 AS ecart_eur
FROM l WHERE price <> items + delivery_fees
ORDER BY abs(items + delivery_fees - price) DESC;


-- ---------------------------------------------------------------------
-- 7. CAUSE : chiffre d'affaires ABSENT du tableau TVA (jointures internes)
--    7a : categories show_in_report = false (masquees en MANUAL)
--    7b : lignes dont produit ou categorie TVA n'existe pas (la ligne disparait sans alerte)
-- ---------------------------------------------------------------------
-- 7a
SELECT tva.tva_id, tva.tva_title, tva.show_in_report, count(*) AS nb_lignes,
       ROUND(SUM((oi.price + COALESCE(e.extra_price,0)) * oi.quantity)/100.0, 2) AS ttc_eur
FROM orders o
JOIN orderitems oi ON oi.order_id = o.order_id
JOIN products p ON p.product_id = oi.product_id
JOIN tva_categories tva ON tva.tva_id = COALESCE(oi.tva_id, CASE
    WHEN o.order_type = 'DELIVERY' THEN p.tva_delivery_id WHEN o.order_type = 'TAKE_AWAY' THEN p.tva_take_away_id ELSE p.tva_in_id END)
LEFT JOIN (SELECT order_item_id, SUM(price) AS extra_price FROM extra GROUP BY order_item_id) e ON e.order_item_id = oi.order_item_id
WHERE o.merchant_id = '212' AND o.brand = 'WELLO_RESTO' AND o.state = 'CLOSED'
  AND upper(o.brand_status) NOT IN ('DELETED','CANCELED','DENIED','DELIVERY_CANCELED','DELIVERY_FAILED')
  AND o.created_by NOT IN ('-1','SCANNORDER')
  AND o.creation_date >= '2026-09-30 22:00:00+00' AND o.creation_date < '2026-10-31 23:00:00+00'
  AND tva.show_in_report = false
GROUP BY 1,2,3 ORDER BY 5 DESC;

-- 7b
SELECT oi.product_id, p.name, o.order_type, oi.tva_id AS tva_ligne, p.tva_in_id, p.tva_take_away_id, p.tva_delivery_id,
       count(*) AS nb_lignes, ROUND(SUM(oi.price * oi.quantity)/100.0, 2) AS ttc_eur_perdu
FROM orders o
JOIN orderitems oi ON oi.order_id = o.order_id
LEFT JOIN products p ON p.product_id = oi.product_id
LEFT JOIN tva_categories tva ON tva.tva_id = COALESCE(oi.tva_id, CASE
    WHEN o.order_type = 'DELIVERY' THEN p.tva_delivery_id WHEN o.order_type = 'TAKE_AWAY' THEN p.tva_take_away_id ELSE p.tva_in_id END)
WHERE o.merchant_id = '212' AND o.brand = 'WELLO_RESTO' AND o.state = 'CLOSED'
  AND upper(o.brand_status) NOT IN ('DELETED','CANCELED','DENIED','DELIVERY_CANCELED','DELIVERY_FAILED')
  AND o.created_by NOT IN ('-1','SCANNORDER')
  AND o.creation_date >= '2026-09-30 22:00:00+00' AND o.creation_date < '2026-10-31 23:00:00+00'
  AND tva.tva_id IS NULL
GROUP BY 1,2,3,4,5,6,7 ORDER BY 9 DESC;


-- ---------------------------------------------------------------------
-- 8. CAUSE : paiements desactives ou hors perimetre
--    8a : paiements enabled = false sur des commandes d'octobre (ignores par le rapport)
--    8b : commandes payees uniquement par une remise (pas de paiement reel -> exclues en AUTO)
-- ---------------------------------------------------------------------
-- 8a
SELECT p.mop, p.status_check, count(*) AS nb, ROUND(SUM(p.amount)/100.0, 2) AS eur
FROM payments p JOIN orders o ON o.order_id = p.order_id
WHERE o.merchant_id = '212' AND o.state = 'CLOSED'
  AND o.creation_date >= '2026-09-30 22:00:00+00' AND o.creation_date < '2026-10-31 23:00:00+00'
  AND p.enabled IS NOT TRUE
GROUP BY 1,2 ORDER BY 4 DESC;

-- 8b
SELECT o.order_id, o.price/100.0 AS price_eur,
       SUM(p.amount) FILTER (WHERE upper(trim(p.mop)) IN ('CURRENCY','PERCENTAGE','DISCOUNT'))/100.0 AS remise_eur
FROM orders o JOIN payments p ON p.order_id = o.order_id AND p.enabled = TRUE
WHERE o.merchant_id = '212' AND o.state = 'CLOSED'
  AND upper(o.brand_status) NOT IN ('DELETED','CANCELED','DENIED','DELIVERY_CANCELED','DELIVERY_FAILED')
  AND o.creation_date >= '2026-09-30 22:00:00+00' AND o.creation_date < '2026-10-31 23:00:00+00'
GROUP BY 1,2
HAVING SUM(p.amount) FILTER (WHERE upper(trim(p.mop)) NOT IN ('CURRENCY','PERCENTAGE','DISCOUNT')) IS NULL;

ROLLBACK;
