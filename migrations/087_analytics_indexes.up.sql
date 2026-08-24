-- Index de support pour la page Analyses et pour les lectures de commande.
--
-- ATTENTION - CE FICHIER NE DOIT PAS ETRE EXECUTE DANS UNE TRANSACTION.
-- CREATE INDEX CONCURRENTLY est refuse par PostgreSQL a l'interieur d'un bloc
-- transactionnel. Les migrations du projet sont appliquees manuellement (cf.
-- CLAUDE.md, "no migration tool"), donc jouer ce fichier instruction par
-- instruction, sans BEGIN/COMMIT autour. CONCURRENTLY evite de poser un
-- ACCESS EXCLUSIVE sur orders / orderitems / payments, qui bloquerait la prise
-- de commande en production le temps de la construction.
--
-- Si une creation echoue en cours de route, l'index reste en etat "invalid" :
-- le supprimer (cf. .down.sql) et rejouer, ne pas laisser un index invalide en
-- place, il est maintenu en ecriture sans jamais servir en lecture.
--
-- Chaque index ci-dessous a ete mesure sur staging par un protocole A/B
-- entrelace (creation/suppression alternees dans une transaction annulee), pour
-- neutraliser le bruit CPU de l'instance. Detail complet et gains chiffres :
-- docs/analytics/PERF-INDEX.md du back-office.
--
-- Deux index candidats ont ete EVALUES PUIS ECARTES, ne pas les reintroduire
-- sans nouvelle mesure :
--   * orders (creation_date) seul          -> x1.01, aucun gain : les requetes
--     transverses restent limitees par le CPU, pas par la lecture.
--   * payments (merchant_id, payment_date) -> x0.96, legerement defavorable :
--     le parcours sequentiel de payments coute moins cher que les acces heap
--     aleatoires induits par l'index sur une plage de 12 mois.
--
-- Aucun index partiel sur le perimetre CA (state / brand_status) tant que la
-- casse de brand_status n'est pas normalisee : 36 lignes en minuscules
-- echappent au predicat et sortiraient silencieusement de l'index.

-- 1) Analyses filtrees par etablissement + fenetre de dates.
--    Forme cible : merchant_id = ANY($1) AND creation_date >= $2
--    Verifie utilise avec ANY() a 1, 5 et 20 merchant_id (multi-etablissement).
--    Gain mesure : x2.06 (CA par jour, 1 etablissement, 12 mois).
--    Sans lui, l'index merchant_id seul lit 18 974 lignes pour en garder 9 864.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_merchant_creation
    ON orders (merchant_id, creation_date);

-- 2) Remontee des lignes d'une commande.
--    La PK est (order_item_id, order_id, product_id) : order_id n'etant pas
--    colonne de tete, aucune recherche par order_id ne peut l'utiliser.
--    Gain mesure : x100 sur la lecture des lignes de 10 commandes.
--    C'est la requete la plus frequente de l'application (affichage commande,
--    impression ticket, ecran de production).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orderitems_order_id
    ON orderitems (order_id);

-- 3) Supplements d'une ligne de commande. Aucun index n'existait : extra_pkey
--    (sur id) affiche 0 parcours depuis la creation de la base.
--    Mesure conjointe avec (2) : x100.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_extra_order_item_id
    ON extra (order_item_id);

-- 4) Reglements d'une commande.
--    Gain mesure : x107 a x570 sur la lecture des paiements de 10 commandes.
--    Nuance : pris ISOLEMENT, cet index degrade la requete analytique de detail
--    des reglements (x0.75) en incitant le planificateur a une boucle imbriquee.
--    Deploye avec (1), l'ensemble mesure x2.41 sur cette meme requete. Les
--    quatre index vont donc ensemble, ne pas en deployer un sous-ensemble.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_payments_order_id
    ON payments (order_id);

-- Rafraichissement des statistiques : sans ANALYZE, le planificateur peut
-- ignorer les index fraichement crees. autovacuum n'a jamais tourne sur ces
-- tables sur staging (pg_stat_user_tables : last_autoanalyze = NULL).
ANALYZE orders;
ANALYZE orderitems;
ANALYZE extra;
ANALYZE payments;
