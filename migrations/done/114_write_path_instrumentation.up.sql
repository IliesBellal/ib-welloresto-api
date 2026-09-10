-- PostgreSQL migration : instrumentation du chemin d'écriture (PROMPT 07 lot 1
-- de ROADMAP-analytics.md — B2, A6, A6b, C2, B3). Migration strictement
-- additive, une seule vague. Toute donnée non déterminable reste NULL — voir
-- docs/decisions.md pour la justification détaillée de chaque règle de
-- rétro-remplissage (dérivations, requêtes de vérification, échantillons
-- staging).
--
-- Aucune FK créée (convention du dépôt, cf.
-- docs/migration-postgres/04-schema-postgres-target.sql).

-- ============================================================================
-- 1. orderitems : coût de revient figé à la vente (B2)
-- ============================================================================
-- Coût UNITAIRE (pas le coût de la ligne) : orderitems.quantity peut changer
-- pendant qu'une commande reste ouverte (UpdateOrder), et un coût unitaire se
-- multiplie trivialement par la quantité au moment de la lecture (mêmes
-- garanties que price/base_price, déjà stockés en unitaire sur cette table).
-- Un coût de ligne pré-multiplié aurait exigé de le recalculer à chaque
-- changement de quantité, dupliquant une règle déjà portée par price/base_price.
--
-- Montant en centimes entiers (integer), jamais en flottant : orders.monnaie
-- est l'exemple à ne pas reproduire dans ce dépôt (real, jamais lu nulle part
-- côté Go après lecture, dérive silencieuse — voir docs/decisions.md).
--
-- NULL est la valeur normale d'un produit sans recette (boisson revendue
-- telle quelle, article négoce...) : cost_price_reason distingue ce cas de
-- l'incident de paramétrage (recette existante mais incomplète : ingrédient
-- sans prix d'achat, unité non convertible, recette vide). Cette colonne n'a
-- de sens que lorsque cost_price_unit est NULL, à une exception près : une
-- ligne écrite avant le déploiement du code de ce lot porte les deux colonnes
-- à NULL (jamais évaluée) — un NULL/NULL structurellement identique à
-- « NO_RECIPE » en apparence, mais sans conséquence puisqu'on ne rétro-calcule
-- jamais l'historique (règle d'or de ce lot).
ALTER TABLE orderitems
    ADD COLUMN IF NOT EXISTS cost_price_unit integer,
    ADD COLUMN IF NOT EXISTS cost_price_reason varchar(20);

ALTER TABLE orderitems
    ADD CONSTRAINT chk_orderitems_cost_price_reason
    CHECK (cost_price_reason IS NULL OR cost_price_reason IN ('NO_RECIPE', 'INCOMPLETE_RECIPE'));

COMMENT ON COLUMN orderitems.cost_price_unit IS 'Coût de revient unitaire (recette du produit + options sélectionnées ayant un ingrédient lié), en centimes entiers, snapshoté au moment de l''écriture de la ligne (insert ou upsert de quantité/prix). Ne change plus jamais après coup, y compris si components.purchase_price est modifié ultérieurement. NULL si non calculable (voir cost_price_reason) ou si la ligne a été écrite avant ce lot.';
COMMENT ON COLUMN orderitems.cost_price_reason IS 'Raison de cost_price_unit NULL : NO_RECIPE (produit sans recette définie — cas normal) ou INCOMPLETE_RECIPE (recette/option existante mais un ingrédient est sans prix d''achat, une conversion d''unité est introuvable, ou la recette est vide — défaut de paramétrage à corriger). NULL si cost_price_unit est renseigné, ou si la ligne est antérieure à ce lot (jamais évaluée).';

-- ============================================================================
-- 2. orders : source de création de la commande (A6)
-- ============================================================================
-- Aucune colonne existante ne porte cette information (vérifié : ni sur
-- orders, ni ailleurs — le marquage actuel est éclaté entre brand,
-- created_by, cash_register_id ; cf. docs/ARCHITECTURE_API.md §7.3 et
-- docs/KIOSK_DECISIONS.md, qui proposait déjà ce champ sans jamais
-- l'implémenter). order_source centralise la dérivation dans une seule
-- colonne au lieu de la refaire ad hoc dans chaque requête.
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS order_source varchar(30);

ALTER TABLE orders
    ADD CONSTRAINT chk_orders_order_source
    CHECK (order_source IS NULL OR order_source IN ('WELLO_RESTO_POS', 'KIOSK', 'SCANNORDER', 'UBER_EATS', 'DELIVEROO'));

COMMENT ON COLUMN orders.order_source IS 'Canal de création de la commande : WELLO_RESTO_POS, KIOSK, SCANNORDER, UBER_EATS, DELIVEROO. Renseigné à l''écriture par le code (order_life_cycle.resolveOrderSource) ; rétro-rempli ci-dessous depuis brand x created_by pour l''historique. NULL quand la dérivation est ambiguë (ex. valeurs de created_by contradictoires avec brand).';

-- Rétro-remplissage intégral (dérivation sans ambiguïté dans l'immense
-- majorité des cas, vérifiée sur staging : 33858/33862 commandes couvertes,
-- soit 99,99% — le reste reste NULL plutôt que deviné). Règle :
--   - brand = UBER_EATS / DELIVEROO  -> le brand lui-même (created_by ne dit
--     rien du canal pour ces deux, juste du webhook qui a créé la ligne).
--   - brand = WELLO_RESTO ET created_by = 'KIOSK'                -> KIOSK
--   - brand = WELLO_RESTO ET created_by IN ('SCANNORDER','-1')   -> SCANNORDER
--     ('-1' est un marqueur ScanNOrder historique, remplacé par 'SCANNORDER'
--     depuis — cf. internal/modules/pos/accounting/repository.go qui traite
--     encore les deux comme équivalents dans ses filtres existants)
--   - brand = WELLO_RESTO ET created_by numérique ou 'user-...'   -> WELLO_RESTO_POS
--     (identifiant d'employé réel, authentifié)
--   - tout le reste (ex. created_by='0' avec brand=WELLO_RESTO : anomalie de
--     données, cash_register_id contredit brand sur les quelques lignes
--     concernées) -> NULL
UPDATE orders SET order_source = CASE
    WHEN brand = 'UBER_EATS' THEN 'UBER_EATS'
    WHEN brand = 'DELIVEROO' THEN 'DELIVEROO'
    WHEN brand = 'WELLO_RESTO' AND created_by = 'KIOSK' THEN 'KIOSK'
    WHEN brand = 'WELLO_RESTO' AND created_by IN ('SCANNORDER', '-1') THEN 'SCANNORDER'
    WHEN brand = 'WELLO_RESTO' AND created_by ~ '^[0-9]+$' AND created_by NOT IN ('0', '-1') THEN 'WELLO_RESTO_POS'
    WHEN brand = 'WELLO_RESTO' AND created_by ~ '^user-' THEN 'WELLO_RESTO_POS'
    ELSE NULL
END
WHERE order_source IS NULL;

-- ============================================================================
-- 3. customer : source d'acquisition (A6b)
-- ============================================================================
-- A ne pas confondre avec customer_brand (existant), qui désigne la marque
-- propriétaire de la fiche client (WELLO_RESTO/UBER_EATS/DELIVEROO), pas
-- l'origine d'acquisition. Non rétro-remplissable par construction (donnée
-- captée uniquement à la création, jamais dérivable après coup depuis l'état
-- actuel du client) : reste NULL sur tout l'historique, y compris pour les
-- clients UBER_EATS/DELIVEROO dont customer_brand suffirait presque à deviner
-- — on ne devine pas.
ALTER TABLE customer
    ADD COLUMN IF NOT EXISTS acquisition_source varchar(30);

COMMENT ON COLUMN customer.acquisition_source IS 'Canal par lequel ce client a été acquis, capté uniquement à la création de la fiche (même vocabulaire que orders.order_source : WELLO_RESTO_POS, KIOSK, SCANNORDER, UBER_EATS, DELIVEROO). Ne pas confondre avec customer_brand (marque propriétaire de la fiche, pas acquisition). NULL pour tout l''historique antérieur à ce lot (non rétro-remplissable) et pour toute fiche créée par un chemin hors périmètre de ce lot (import, réservation...).';

-- ============================================================================
-- 4. orders : typologie d'auteur d'annulation (C2)
-- ============================================================================
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS cancelled_by_type varchar(20);

ALTER TABLE orders
    ADD CONSTRAINT chk_orders_cancelled_by_type
    CHECK (cancelled_by_type IS NULL OR cancelled_by_type IN ('STAFF', 'CUSTOMER', 'SYSTEM', 'PLATFORM'));

COMMENT ON COLUMN orders.cancelled_by_type IS 'Typologie de l''auteur d''annulation/refus : STAFF (employé identifié, via order_life_cycle.DenyOrderLocal/DeleteOrderLocal), CUSTOMER (self-service ScanNOrder/Kiosk), SYSTEM (automatique : cron d''expiration, échec/expiration de paiement), PLATFORM (Uber Eats ou Deliveroo). NULL pour une commande jamais annulée, ou pour une annulation historique dont l''auteur n''est pas déterminable sans deviner. Voir docs/decisions.md pour le recensement exhaustif des chemins d''annulation et leur classification.';

-- Rétro-remplissage des commandes déjà refusées/annulées, à partir de brand +
-- deletion_reason_id (varchar(11) — certaines lignes historiques portent la
-- valeur entre guillemets littéraux, ex. "'3'" au lieu de "3" : artefact d'un
-- ancien chemin d'écriture, normalisé ici avec trim() pour ne pas perdre ce
-- signal) + created_by. Vérifié sur staging : 2225 commandes annulées/refusées
-- au total, couverture STAFF=1270 SYSTEM=485 PLATFORM=145 CUSTOMER=27 NULL=298
-- (13,4% NULL — laissé tel quel plutôt que deviné, cf. docs/decisions.md pour
-- le détail ligne par ligne de chaque cas ambigu).
--   - brand = DELIVEROO -> PLATFORM systématiquement. Aucun chemin de ce dépôt
--     ne permet à un employé d'annuler une commande Deliveroo depuis le POS
--     (contrairement à Uber Eats) : toute mutation de brand_status sur une
--     commande Deliveroo provient du webhook (internal/webhook/deliveroo_orders).
--   - brand = UBER_EATS ET deletion_reason_id (déguillemetté) IN ('39','41')
--     -> PLATFORM (39 = UBER_EATS_WEBHOOK "Deleted by Uber Eats" ; 41 =
--     ACCEPT_TIMED_OUT "You didn't accept this order", reconciliation vers
--     l'état Uber côté RecoverOrderState). deletion_reason_id dans le
--     catalogue uber_eats_cancel/uber_eats_deny (12-26, 28-34) -> STAFF
--     (raison choisie par l'employé pour propager l'annulation/le refus vers
--     l'API Uber Eats, cf. internal/modules/ubereats/service.go DenyOrder/
--     CancelOrder). created_by ne distingue PAS ce cas de PLATFORM pour
--     UBER_EATS : il porte toujours l'identité du webhook qui a créé la
--     commande, jamais celle de qui l'a annulée ensuite.
--   - brand = WELLO_RESTO ET created_by = 'KIOSK' -> CUSTOMER (self-service
--     kiosk). deletion_reason_id IN ('KIOSK_CUSTOMER_CANCELLED',
--     'SNO_CUSTOMER_CANCELLED') ou leurs variantes tronquées à 11 caractères
--     ('KIOSK_CUSTO', 'SNO_CUSTOME' — orders.deletion_reason_id est
--     varchar(11), les sentinelles Go plus longues sont silencieusement
--     tronquées à l'écriture ; bug préexistant hors périmètre de ce lot, noté
--     ici pour mémoire) -> CUSTOMER.
--   - brand = WELLO_RESTO ET deletion_reason_id (déguillemetté) IN ('42','43')
--     -> SYSTEM (expiration automatique : délai d'approbation dépassé pour
--     '42', session de paiement en ligne expirée/annulée pour '43' — cron
--     DenyOrders et webhook Stripe checkout.session.expired partagent ce
--     bucket, les deux sont des mécanismes automatiques).
--   - brand = WELLO_RESTO, created_by un identifiant d'employé réel (numérique
--     ou 'user-...') ET deletion_reason_id dans le catalogue générique 'order'
--     (motifs 1-8 : Produits indisponibles, Non-paiement, Annulation client
--     rapportée par le staff, Refus de service, Problème technique, Non-
--     conformité réglementaire, Client parti sans payer, Autre) -> STAFF.
--   - tout le reste (deletion_reason_id absent/à '0', ou combinaison non
--     couverte ci-dessus) -> NULL.
UPDATE orders SET cancelled_by_type = CASE
    WHEN brand = 'DELIVEROO' THEN 'PLATFORM'
    WHEN brand = 'UBER_EATS' AND trim(both '''' from deletion_reason_id) IN ('39', '41') THEN 'PLATFORM'
    WHEN brand = 'UBER_EATS' AND trim(both '''' from deletion_reason_id) IN (
            SELECT deletion_reason_id::text FROM deletion_reasons
            WHERE deletion_reason_object IN ('uber_eats_cancel', 'uber_eats_deny')
        ) THEN 'STAFF'
    WHEN brand = 'WELLO_RESTO' AND created_by = 'KIOSK' THEN 'CUSTOMER'
    WHEN brand = 'WELLO_RESTO' AND trim(both '''' from deletion_reason_id) IN
        ('SNO_CUSTOMER_CANCELLED', 'KIOSK_CUSTOMER_CANCELLED', 'SNO_CUSTOME', 'KIOSK_CUSTO') THEN 'CUSTOMER'
    WHEN brand = 'WELLO_RESTO' AND trim(both '''' from deletion_reason_id) IN ('42', '43') THEN 'SYSTEM'
    WHEN brand = 'WELLO_RESTO' AND created_by ~ '^[0-9]+$' AND created_by NOT IN ('0', '-1')
        AND trim(both '''' from deletion_reason_id) IN (
            SELECT deletion_reason_id::text FROM deletion_reasons WHERE deletion_reason_object = 'order'
        ) THEN 'STAFF'
    WHEN brand = 'WELLO_RESTO' AND created_by ~ '^user-'
        AND trim(both '''' from deletion_reason_id) IN (
            SELECT deletion_reason_id::text FROM deletion_reasons WHERE deletion_reason_object = 'order'
        ) THEN 'STAFF'
    ELSE NULL
END
WHERE upper(brand_status) IN ('DENIED', 'CANCELED', 'REJECTED')
  AND cancelled_by_type IS NULL;

-- ============================================================================
-- 5. orders : normalisation de brand_status en majuscules (B3)
-- ============================================================================
-- Prérequis à tout index partiel sur le périmètre CA (non créé dans ce lot) :
-- un prédicat sensible à la casse laisserait des commandes annulées entrer
-- silencieusement dans le chiffre d'affaires. Seul le webhook Deliveroo
-- écrit en minuscules aujourd'hui (internal/webhook/deliveroo_orders) ; le
-- code Go de ce lot le corrige pour écrire en majuscules désormais (voir
-- docs/decisions.md). Cette UPDATE ne rattrape que l'historique.
-- Vérifié sur staging : 36 lignes concernées (accepted:14 canceled:11
-- rejected:10 placed:1, toutes brand=DELIVEROO) — cohérent avec les 36
-- lignes citées par le brief pour le périmètre total.
UPDATE orders SET brand_status = upper(brand_status)
WHERE brand_status <> upper(brand_status);
