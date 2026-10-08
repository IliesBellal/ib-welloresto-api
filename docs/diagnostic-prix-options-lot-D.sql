-- Diagnostic lecture seule (production), lot D conformité caisse, 2026-10-08.
-- Voir docs/attestation-conformite-05-lot-D-brief.md, phase 1, « Prix calculé
-- par la caisse ».
--
-- Question : les écarts entre orders.price (TTC transmis par la caisse) et la
-- somme des lignes enregistrées s'expliquent-ils par le surcoût des options
-- payantes, que le serveur n'enregistrait pas avant la migration 173 ?
--
-- Pour chaque commande de vente close des 12 derniers mois :
--   lignes_sans_options = Σ (prix + suppléments) × quantité + frais de livraison
--     (la formule du ticket et de l'export comptable avant le lot D) ;
--   lignes_avec_options = la même chose + Σ surcoût catalogue des options
--     × quantité de l'option × quantité de l'article (approximation : prix du
--     catalogue aujourd'hui, faute de prix figé avant la migration 173).
-- Les remises de caisse sont des paiements : elles n'entrent pas dans
-- orders.price et ne sont pas comptées ici.
--
-- Aucune donnée personnelle : agrégats par mois et par canal seulement.
-- Lecture seule : à lancer dans une transaction READ ONLY.

BEGIN TRANSACTION READ ONLY;

WITH opt AS (
    SELECT oic.order_item_id,
           SUM(oic.quantity * COALESCE(cao.extra_price, 0)) AS surcout
    FROM order_item_configuration oic
    JOIN configurable_attribute_options cao ON cao.id = oic.configuration_attribute_option_id
    GROUP BY oic.order_item_id
), ext AS (
    SELECT order_item_id, SUM(price) AS supplements
    FROM extra
    GROUP BY order_item_id
), lignes AS (
    SELECT oi.order_id,
           SUM((oi.price + COALESCE(ext.supplements, 0)) * oi.quantity) AS sans_options,
           SUM((oi.price + COALESCE(ext.supplements, 0) + COALESCE(opt.surcout, 0)) * oi.quantity) AS avec_options,
           bool_or(COALESCE(opt.surcout, 0) <> 0) AS a_options_payantes
    FROM orderitems oi
    LEFT JOIN ext ON ext.order_item_id = oi.order_item_id
    LEFT JOIN opt ON opt.order_item_id = oi.order_item_id
    GROUP BY oi.order_id
), cmd AS (
    SELECT date_trunc('month', o.creation_date)::date AS mois,
           COALESCE(o.order_source, o.brand, '?') AS canal,
           o.price,
           l.sans_options + COALESCE(o.delivery_fees, 0) AS sans_options,
           l.avec_options + COALESCE(o.delivery_fees, 0) AS avec_options,
           l.a_options_payantes
    FROM orders o
    JOIN lignes l ON l.order_id = o.order_id
    WHERE o.state = 'CLOSED'
      AND upper(o.brand_status) NOT IN ('DELETED', 'CANCELED', 'DENIED', 'DELIVERY_CANCELED', 'DELIVERY_FAILED')
      AND o.creation_date >= now() - interval '12 months'
)
SELECT mois,
       canal,
       count(*)                                                        AS commandes,
       count(*) FILTER (WHERE a_options_payantes)                      AS avec_options_payantes,
       count(*) FILTER (WHERE price <> sans_options)                   AS ecart_formule_actuelle,
       count(*) FILTER (WHERE price <> avec_options)                   AS ecart_options_comprises,
       count(*) FILTER (WHERE price <> sans_options AND price = avec_options) AS ecart_explique_par_les_options,
       round(sum(abs(price - sans_options)) / 100.0, 2)                AS ecart_eur_formule_actuelle,
       round(sum(abs(price - avec_options)) / 100.0, 2)                AS ecart_eur_options_comprises
FROM cmd
GROUP BY mois, canal
ORDER BY mois, canal;

ROLLBACK;
