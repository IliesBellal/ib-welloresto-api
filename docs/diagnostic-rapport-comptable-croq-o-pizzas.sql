-- =====================================================================
-- DIAGNOSTIC — Rapport comptable Croq'Ô'Pizzas (merchant 212)
-- Septembre 2026 — 100 % READ-ONLY
-- (version juillet/août : historique git, commit f349794)
--
-- Exécuter d'un bloc. Chaque section est numérotée et son résultat
-- attendu / son interprétation sont décrits en commentaire.
--
-- Filtres alignés sur le code DÉPLOYÉ en prod (origin/main,
-- internal/modules/pos/accounting/repository.go) : GetTVAData,
-- GetTrustedEnclosedRegisterIDs, GetRealPaymentsData,
-- filterExcludedPaymentLabels.
--
-- Bornes : le rapport PDF travaille en heure locale Europe/Paris.
-- En septembre 2026 Paris = UTC+2 (passage à l'heure d'hiver le 25/10), donc :
--   Septembre : [2026-08-31 22:00Z , 2026-09-30 22:00Z[
--
-- PDF à expliquer (généré le 2026-10-08 13:15) :
--   TVA TTC        : 63.00 + 9339.97 + 690.40          = 10093.37 EUR
--   Encaissements  : 443.67 + 9932.89 + 96.46          = 10473.02 EUR
--   Écart          : 379.65 EUR d'encaissements en trop
-- =====================================================================

BEGIN;
SET TRANSACTION READ ONLY;

-- ---------------------------------------------------------------------
-- 0. Contrôles d'environnement
-- ---------------------------------------------------------------------
SHOW timezone;
-- Le code passe ses bornes en texte 'YYYY-MM-DD HH:MM:SS' sans fuseau : la
-- base les lit dans le fuseau de SA session. Si ce n'est pas UTC, le PDF a
-- été calculé sur une période décalée et ce script (bornes en +00) ne le
-- reproduira pas à l'identique.

SELECT id, fullname, siret, timezone FROM merchant WHERE id = 212;

SELECT tva_id, delivery_type, tva_title, tva_rate, show_in_report, enabled
FROM tva_categories ORDER BY tva_id;


-- ---------------------------------------------------------------------
-- 1. CASCADE DU PÉRIMÈTRE — où part le chiffre d'affaires
--    Montre l'effet de chaque filtre du tableau TVA, en euros.
-- ---------------------------------------------------------------------
WITH base AS (
  SELECT o.*
  FROM orders o
  WHERE o.merchant_id = '212'
    AND o.creation_date >= '2026-08-31 22:00:00+00'
    AND o.creation_date <  '2026-09-30 22:00:00+00'
)
SELECT etape, nb, ROUND(eur, 2) AS eur FROM (
  SELECT '1. toutes commandes (tous canaux)' AS etape, 1 AS ord,
         count(*) AS nb, SUM(price)/100.0 AS eur FROM base
  UNION ALL
  SELECT '2. brand = WELLO_RESTO', 2, count(*), SUM(price)/100.0
  FROM base WHERE brand='WELLO_RESTO'
  UNION ALL
  SELECT '3. + state = CLOSED', 3, count(*), SUM(price)/100.0
  FROM base WHERE brand='WELLO_RESTO' AND state='CLOSED'
  UNION ALL
  SELECT '4. + hors DELETED/CANCELED/DELIVERY_CANCELED/DELIVERY_FAILED', 4,
         count(*), SUM(price)/100.0
  FROM base WHERE brand='WELLO_RESTO' AND state='CLOSED'
    AND brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED')
  UNION ALL
  SELECT '5. + hors created_by -1/SCANNORDER  <= PERIMETRE RAPPORT', 5,
         count(*), SUM(price)/100.0
  FROM base WHERE brand='WELLO_RESTO' AND state='CLOSED'
    AND brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED')
    AND created_by NOT IN ('-1','SCANNORDER')
) x ORDER BY ord;
-- ATTENDU : l'étape 5 (orders.price) devrait être proche de 10093.37 EUR.
-- L'écart entre l'étape 5 et le TTC du tableau TVA (section 2) vient des
-- sections 5 et 6 ci-dessous (prix incohérent, lignes perdues).


-- ---------------------------------------------------------------------
-- 2. REPRODUCTION EXACTE DU TABLEAU TVA DU PDF (requête GetTVAData)
-- ---------------------------------------------------------------------
WITH src AS (
  SELECT tva.tva_title AS titre, tva.tva_rate::numeric AS taux,
         ((oi.price + COALESCE(e.extra_price,0)) * oi.quantity)::numeric AS ttc
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
  WHERE o.merchant_id='212' AND o.state='CLOSED' AND o.brand='WELLO_RESTO'
    AND o.brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED')
    AND o.created_by NOT IN ('-1','SCANNORDER')
    AND tva.show_in_report
    AND o.creation_date >= '2026-08-31 22:00:00+00'
    AND o.creation_date <  '2026-09-30 22:00:00+00'
  UNION ALL
  SELECT tf.tva_title, tf.tva_rate::numeric, o.delivery_fees::numeric
  FROM orders o
  JOIN tva_categories tf ON tf.tva_id = -1
  WHERE o.merchant_id='212' AND o.state='CLOSED' AND o.brand='WELLO_RESTO'
    AND o.brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED')
    AND o.created_by NOT IN ('-1','SCANNORDER')
    AND o.creation_date >= '2026-08-31 22:00:00+00'
    AND o.creation_date <  '2026-09-30 22:00:00+00'
)
SELECT titre, taux,
       ROUND(SUM(ttc)*(100.0/(100.0+taux))/100.0, 2) AS ht_eur,
       ROUND((SUM(ttc) - SUM(ttc)*(100.0/(100.0+taux)))/100.0, 2) AS tva_eur,
       ROUND(SUM(ttc)/100.0, 2) AS ttc_eur
FROM src GROUP BY titre, taux ORDER BY titre;
-- ATTENDU, ligne pour ligne :
--   TVA Delivery fees 20% : 52.50 / 10.50 / 63.00
--   TVA 10%               : 8490.88 / 849.09 / 9339.97
--   TVA 5.5%              : 654.41 / 35.99 / 690.40


-- ---------------------------------------------------------------------
-- 3. SECTION ENCAISSEMENTS
--    3a : reproduction exacte du PDF (registres de confiance uniquement)
--    3b : registres ÉCARTÉS par le contrôle de dérive
--    3c : réel saisi vs théorique (payments), registre par registre
-- ---------------------------------------------------------------------
-- 3a
WITH reg AS (
  SELECT cash_register_id FROM cash_registers
  WHERE merchant_id='212' AND enclosed = TRUE
    AND start_date >= '2026-08-31 22:00:00+00'
    AND start_date <  '2026-09-30 22:00:00+00'
),
fige AS (
  SELECT cri.cash_register_id, cri.mop, SUM(cri.amount) AS montant
  FROM cash_registers_items cri JOIN reg USING (cash_register_id)
  GROUP BY 1,2
),
live AS (
  SELECT p.cash_register_id::int AS cash_register_id, p.mop, SUM(p.amount) AS montant
  FROM payments p
  JOIN orders o ON o.order_id = p.order_id
  JOIN reg ON reg.cash_register_id::text = p.cash_register_id
  WHERE o.brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED')
    AND p.enabled IS TRUE
  GROUP BY 1,2
),
derive AS (
  SELECT DISTINCT COALESCE(f.cash_register_id, v.cash_register_id) AS cash_register_id
  FROM fige f
  FULL OUTER JOIN live v
    ON v.cash_register_id = f.cash_register_id AND v.mop = f.mop
  WHERE COALESCE(f.montant,-1) <> COALESCE(v.montant,-1)
),
reel AS (
  SELECT COALESCE(NULLIF(l.label,''), crci.label) AS libelle, crci.amount
  FROM cash_registers_custom_items crci
  JOIN reg USING (cash_register_id)
  LEFT JOIN labels l ON l.label_type='mop' AND l.label_value=crci.label AND l.lang='FR'
  WHERE crci.enabled = TRUE
    AND crci.label NOT IN ('STRIPE','UBER_EATS','DELIVEROO')
    AND crci.cash_register_id NOT IN (SELECT cash_register_id FROM derive)
)
SELECT libelle, ROUND(SUM(amount)/100.0, 2) AS eur
FROM reel
WHERE upper(trim(libelle)) NOT IN ('UBER EATS','DELIVEROO','SCANNORDER')
GROUP BY libelle
HAVING SUM(amount) <> 0
ORDER BY libelle;
-- ATTENDU : Borne de commande 443.67 / Carte bancaire 9932.89 / Espèce 96.46

-- 3b : chaque registre listé ici est ENTIÈREMENT absent des encaissements
--      du PDF, sans aucune mention sur le document (seulement un log WARN).
WITH reg AS (
  SELECT cash_register_id FROM cash_registers
  WHERE merchant_id='212' AND enclosed = TRUE
    AND start_date >= '2026-08-31 22:00:00+00'
    AND start_date <  '2026-09-30 22:00:00+00'
),
fige AS (
  SELECT cri.cash_register_id, cri.mop, SUM(cri.amount) AS montant
  FROM cash_registers_items cri JOIN reg USING (cash_register_id)
  GROUP BY 1,2
),
live AS (
  SELECT p.cash_register_id::int AS cash_register_id, p.mop, SUM(p.amount) AS montant
  FROM payments p
  JOIN orders o ON o.order_id = p.order_id
  JOIN reg ON reg.cash_register_id::text = p.cash_register_id
  WHERE o.brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED')
    AND p.enabled IS TRUE
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

-- 3c : CAUSE 1 — saisie de clôture. Pour chaque registre de septembre :
--      réel saisi par le restaurateur (custom items, ce que lit le PDF)
--      contre le théorique FIGÉ par le POS à la clôture (cash_registers_items,
--      produit par cashRegisterReportMOPSQL, cash_registers/repository.go:154 —
--      TOUS les mops, aucune exclusion STRIPE/UBER_EATS/DELIVEROO ni filtre de
--      brand/état sur les commandes).
--      /!\ Version précédente de cette requête (corrigée ici) : elle
--      recalculait un "théorique" en direct depuis payments en excluant
--      STRIPE/UBER_EATS/DELIVEROO — un filtre qui n'existe dans AUCUN chemin
--      réel du code. Les paiements de ces mops sans registre encore assigné
--      sont au contraire explicitement requalifiés vers le PROCHAIN registre
--      qui se ferme (cash_registers/repository.go:404-430, étapes 2/3/3bis) et
--      entrent donc dans le théorique figé de ce registre. L'ancienne requête
--      sous-comptait le théorique et fabriquait de faux écarts — d'où le
--      contraste avec l'écart à 0 affiché sur le POS, qui compare réel et
--      théorique figé, pas réel et un recalcul inventé.
--      ecart_eur > 0 = le réel déclaré dépasse le théorique figé par le POS.
WITH reg AS (
  SELECT cash_register_id, start_date, end_date FROM cash_registers
  WHERE merchant_id='212' AND enclosed = TRUE
    AND start_date >= '2026-08-31 22:00:00+00'
    AND start_date <  '2026-09-30 22:00:00+00'
),
reel AS (
  SELECT crci.cash_register_id, SUM(crci.amount) AS montant
  FROM cash_registers_custom_items crci JOIN reg USING (cash_register_id)
  WHERE crci.enabled = TRUE
    AND crci.label NOT IN ('STRIPE','UBER_EATS','DELIVEROO')
  GROUP BY 1
),
fige AS (
  SELECT cri.cash_register_id, SUM(cri.amount) AS montant
  FROM cash_registers_items cri JOIN reg USING (cash_register_id)
  WHERE cri.mop NOT IN ('STRIPE','UBER_EATS','DELIVEROO')
  GROUP BY 1
),
fige_tous_mops AS (
  SELECT cri.cash_register_id, SUM(cri.amount) AS montant
  FROM cash_registers_items cri JOIN reg USING (cash_register_id)
  GROUP BY 1
)
SELECT reg.cash_register_id,
       reg.start_date AT TIME ZONE 'Europe/Paris' AS ouverture_locale,
       reg.end_date   AT TIME ZONE 'Europe/Paris' AS fermeture_locale,
       COALESCE(reel.montant,0)/100.0 AS reel_eur,
       COALESCE(fige.montant,0)/100.0 AS theorique_fige_eur,
       (COALESCE(reel.montant,0) - COALESCE(fige.montant,0))/100.0 AS ecart_eur,
       COALESCE(fige_tous_mops.montant,0)/100.0 AS theorique_fige_tous_mops_eur
FROM reg
LEFT JOIN reel USING (cash_register_id)
LEFT JOIN fige USING (cash_register_id)
LEFT JOIN fige_tous_mops USING (cash_register_id)
ORDER BY abs(COALESCE(reel.montant,0) - COALESCE(fige.montant,0)) DESC;
-- ATTENDU si le POS a raison : ecart_eur proche de 0 partout (hors
-- d'éventuels écarts de caisse réels, désormais plausibles et petits).
-- La dernière colonne (tous mops confondus) sert à voir si
-- STRIPE/UBER_EATS/DELIVEROO pèsent beaucoup sur un registre donné —
-- signe d'une requalification de paiements d'un autre canal.


-- ---------------------------------------------------------------------
-- 4. CAUSE 2 — registres et commandes à cheval sur deux mois
--    Le tableau TVA prend les commandes CRÉÉES en septembre ; les
--    encaissements prennent les registres OUVERTS en septembre.
--    4a : paiements des registres de septembre sur des commandes d'un
--         autre mois (comptés en encaissements, absents de la TVA)
--    4b : paiements des commandes de septembre, selon le registre qui les
--         porte (seule la ligne « registre de septembre » peut être dans le
--         PDF ; les autres sont dans la TVA sans contrepartie encaissée)
-- ---------------------------------------------------------------------
-- 4a
SELECT to_char(o.creation_date AT TIME ZONE 'Europe/Paris','YYYY-MM-DD') AS jour_commande,
       p.cash_register_id, count(DISTINCT o.order_id) AS nb_commandes,
       ROUND(SUM(p.amount)/100.0, 2) AS eur
FROM payments p
JOIN orders o ON o.order_id = p.order_id
JOIN cash_registers cr ON cr.cash_register_id::text = p.cash_register_id
WHERE cr.merchant_id='212' AND cr.enclosed = TRUE
  AND cr.start_date >= '2026-08-31 22:00:00+00'
  AND cr.start_date <  '2026-09-30 22:00:00+00'
  AND p.enabled IS TRUE
  AND (o.creation_date <  '2026-08-31 22:00:00+00'
    OR o.creation_date >= '2026-09-30 22:00:00+00')
GROUP BY 1,2 ORDER BY 1,2;

-- 4b
SELECT CASE
         WHEN cr.cash_register_id IS NULL
           THEN 'aucun registre (' || COALESCE(p.cash_register_id,'NULL') || ')'
         WHEN NOT cr.enclosed THEN 'registre non validé'
         WHEN cr.start_date <  '2026-08-31 22:00:00+00'
           OR cr.start_date >= '2026-09-30 22:00:00+00' THEN 'registre ouvert hors septembre'
         ELSE 'registre de septembre'
       END AS rattachement,
       p.mop, count(*) AS nb, ROUND(SUM(p.amount)/100.0, 2) AS eur
FROM payments p
JOIN orders o ON o.order_id = p.order_id
LEFT JOIN cash_registers cr ON cr.cash_register_id::text = p.cash_register_id
WHERE o.merchant_id='212' AND o.state='CLOSED' AND o.brand='WELLO_RESTO'
  AND o.brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED')
  AND o.created_by NOT IN ('-1','SCANNORDER')
  AND o.creation_date >= '2026-08-31 22:00:00+00'
  AND o.creation_date <  '2026-09-30 22:00:00+00'
  AND p.enabled IS TRUE
GROUP BY 1,2 ORDER BY 1,2;
-- Les mop CURRENCY / PERCENTAGE / DISCOUNT sont des remises, pas de l'argent :
-- le code déployé ne les déduit pas de la base TVA.


-- ---------------------------------------------------------------------
-- 5. CAUSE 3 — commandes dont le prix ne correspond pas à leurs lignes
--    (orders.price <> somme des lignes + frais de livraison)
--    ecart_eur < 0 : le client a payé plus que ce qui est soumis à TVA,
--    typiquement le surcoût des options non enregistré sur la ligne
--    (constat du lot D, docs/attestation-conformite-05-lot-D-brief.md).
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
    AND o.brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED')
    AND o.created_by NOT IN ('-1','SCANNORDER')
    AND o.creation_date >= '2026-08-31 22:00:00+00'
    AND o.creation_date <  '2026-09-30 22:00:00+00'
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
-- Somme de ecart_eur = écart entre orders.price et le TTC du tableau TVA
-- dû aux lignes elles-mêmes.


-- ---------------------------------------------------------------------
-- 6. CAUSE 4 — chiffre d'affaires silencieusement ABSENT du tableau TVA
--    6a : produits rattachés à une catégorie show_in_report = false
--         (typiquement tva_id = 0 « TVA Undefined »)
--    6b : produits dont le tva_id n'existe pas dans tva_categories
--         (INNER JOIN => la ligne disparaît, sans aucune alerte)
-- ---------------------------------------------------------------------
-- 6a
SELECT tva.tva_id, tva.tva_title, tva.show_in_report,
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
  AND o.brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED')
  AND o.created_by NOT IN ('-1','SCANNORDER')
  AND o.creation_date >= '2026-08-31 22:00:00+00'
  AND o.creation_date <  '2026-09-30 22:00:00+00'
  AND tva.show_in_report = false
GROUP BY 1,2,3 ORDER BY 5 DESC;

-- 6b (orderitems sans produit correspondant : LEFT JOIN products, p.product_id NULL)
SELECT oi.product_id, p.name, o.order_type,
       p.tva_in_id, p.tva_take_away_id, p.tva_delivery_id,
       count(*) AS nb_lignes,
       ROUND(SUM(oi.price * oi.quantity)/100.0, 2) AS ttc_eur_perdu
FROM orders o
JOIN orderitems oi ON oi.order_id = o.order_id
LEFT JOIN products p ON p.product_id = oi.product_id
LEFT JOIN tva_categories tva ON tva.tva_id = (CASE
    WHEN o.order_type='DELIVERY'  THEN p.tva_delivery_id
    WHEN o.order_type='TAKE_AWAY' THEN p.tva_take_away_id
    ELSE p.tva_in_id END)
WHERE o.merchant_id='212' AND o.brand='WELLO_RESTO' AND o.state='CLOSED'
  AND o.brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED')
  AND o.created_by NOT IN ('-1','SCANNORDER')
  AND o.creation_date >= '2026-08-31 22:00:00+00'
  AND o.creation_date <  '2026-09-30 22:00:00+00'
  AND tva.tva_id IS NULL
GROUP BY 1,2,3,4,5,6 ORDER BY 8 DESC;


-- ---------------------------------------------------------------------
-- 7. Commandes comptées en TVA malgré un brand_status douteux
--    Le code déployé garde DENIED, PENDING, ONLINE_PAYMENT_PENDING...
--    (DENIED est exclu par le commit 545e9d0, pas encore en prod).
-- ---------------------------------------------------------------------
SELECT brand_status, count(*) AS nb, ROUND(SUM(price)/100.0, 2) AS eur
FROM orders
WHERE merchant_id='212' AND brand='WELLO_RESTO' AND state='CLOSED'
  AND brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED')
  AND created_by NOT IN ('-1','SCANNORDER')
  AND creation_date >= '2026-08-31 22:00:00+00'
  AND creation_date <  '2026-09-30 22:00:00+00'
GROUP BY 1 ORDER BY 3 DESC;
-- Toute valeur autre que CLOSED / DONE mérite un arbitrage comptable.


-- ---------------------------------------------------------------------
-- 8. Mauvais taux de TVA appliqué
--    8a : order_type NULL => le code retombe sur le taux « sur place »
--    8b : frais de livraison facturés sur des commandes non-DELIVERY
-- ---------------------------------------------------------------------
-- 8a
SELECT COALESCE(order_type,'(NULL)') AS order_type,
       count(*) AS nb, ROUND(SUM(price)/100.0, 2) AS eur
FROM orders
WHERE merchant_id='212' AND brand='WELLO_RESTO' AND state='CLOSED'
  AND brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED')
  AND created_by NOT IN ('-1','SCANNORDER')
  AND creation_date >= '2026-08-31 22:00:00+00'
  AND creation_date <  '2026-09-30 22:00:00+00'
GROUP BY 1 ORDER BY 3 DESC;

-- 8b
SELECT COALESCE(order_type,'(NULL)') AS order_type,
       count(*) AS nb, ROUND(SUM(delivery_fees)/100.0, 2) AS frais_eur
FROM orders
WHERE merchant_id='212' AND brand='WELLO_RESTO' AND state='CLOSED'
  AND brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED')
  AND created_by NOT IN ('-1','SCANNORDER')
  AND delivery_fees <> 0
  AND creation_date >= '2026-08-31 22:00:00+00'
  AND creation_date <  '2026-09-30 22:00:00+00'
GROUP BY 1 ORDER BY 1;
-- Toute ligne order_type <> 'DELIVERY' ici = frais de port taxés à 20 %
-- sur une commande qui n'est pas une livraison.


-- ---------------------------------------------------------------------
-- 9. Extras mal comptés
--    nb_qty_sup_1 : extras avec quantity > 1 (le code somme price en
--                   ignorant quantity)
--    nb_item_null : extras dont order_item_id est NULL (jamais rattachés)
-- ---------------------------------------------------------------------
SELECT count(*) FILTER (WHERE ex.quantity > 1) AS nb_qty_sup_1,
       ROUND(COALESCE(SUM(ex.price*(ex.quantity-1)) FILTER (WHERE ex.quantity > 1),0)/100.0, 2)
         AS eur_non_compte,
       count(*) FILTER (WHERE ex.order_item_id IS NULL) AS nb_item_null,
       ROUND(COALESCE(SUM(ex.price) FILTER (WHERE ex.order_item_id IS NULL),0)/100.0, 2)
         AS eur_perdu
FROM extra ex
JOIN orders o ON o.order_id = ex.order_id
WHERE o.merchant_id='212' AND o.brand='WELLO_RESTO' AND o.state='CLOSED'
  AND o.brand_status NOT IN ('DELETED','CANCELED','DELIVERY_CANCELED','DELIVERY_FAILED')
  AND o.created_by NOT IN ('-1','SCANNORDER')
  AND o.creation_date >= '2026-08-31 22:00:00+00'
  AND o.creation_date <  '2026-09-30 22:00:00+00';


-- ---------------------------------------------------------------------
-- 10. CAUSE 1 (suite) — d'où vient l'écart entre le réel "brut" par
--     registre (section 3c, ~4800 EUR de bruit) et le réel qui finit sur
--     le PDF (section 3a, 10473.02 EUR) : quels libellés de
--     cash_registers_custom_items sont retirés par filterExcludedPaymentLabels
--     (comparaison sur le libellé FR affiché, pas sur le code brut déjà
--     filtré par GetRealPaymentsData) et combien ça représente.
-- ---------------------------------------------------------------------
WITH reg AS (
  SELECT cash_register_id FROM cash_registers
  WHERE merchant_id='212' AND enclosed = TRUE
    AND start_date >= '2026-08-31 22:00:00+00'
    AND start_date <  '2026-09-30 22:00:00+00'
)
SELECT crci.label AS code_brut,
       COALESCE(NULLIF(l.label,''), crci.label) AS libelle_fr,
       count(*) AS nb, ROUND(SUM(crci.amount)/100.0, 2) AS eur
FROM cash_registers_custom_items crci
JOIN reg USING (cash_register_id)
LEFT JOIN labels l ON l.label_type='mop' AND l.label_value=crci.label AND l.lang='FR'
WHERE crci.enabled = TRUE
GROUP BY 1,2
ORDER BY 4 DESC;
-- Toute ligne dont libelle_fr (en majuscule) vaut 'UBER EATS', 'DELIVEROO'
-- ou 'SCANNORDER' est retirée du PDF par le code service (service.go,
-- filterExcludedPaymentLabels), sans jamais apparaître ni dans le tableau
-- ni dans aucun message du rapport.

ROLLBACK;
