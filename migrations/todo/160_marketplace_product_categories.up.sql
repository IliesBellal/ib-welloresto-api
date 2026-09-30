-- Produits créés automatiquement à la réception d'une commande marketplace
-- contenant un article inconnu : on remplace leur catégorie tampon par une
-- catégorie « fantôme » qui dit d'où ils viennent (models.MarketplaceCategory*).
-- Ces valeurs n'ont volontairement pas de ligne productcateg : les produits
-- restent hors caisse / Scan&Order / borne / synchro Uber, et l'analyse
-- produits les affiche « Uber Eats » / « Deliveroo ».
--
-- État relevé en prod avant migration :
--   UBER_EATS_TEMP : 310 produits (5 marchands) — seul CreateExternalProductTx
--                    écrit cette valeur, donc tous d'origine Uber, y compris
--                    les 11 dont le mapping a disparu depuis ;
--   ''             : 211 produits (5 marchands), dont 66 mappés Deliveroo
--                    (INSERT historique sans catégorie). Les 145 autres sont
--                    d'origine inconnue et NE SONT PAS touchés ici.
--
-- Idempotente : peut être rejouée après le déploiement du code pour rattraper
-- un produit créé entre-temps par l'ancien code.

UPDATE products
SET category = 'UBER_EATS'
WHERE category = 'UBER_EATS_TEMP';

-- Uniquement les produits mappés Deliveroo dont la catégorie ne pointe sur
-- aucune productcateg du marchand : un produit que le marchand a déjà rangé
-- dans une vraie catégorie garde la sienne.
UPDATE products p
SET category = 'DELIVEROO'
WHERE EXISTS (
        SELECT 1
        FROM integration_deliveroo_products_mapping m
        WHERE m.merchant_id = p.merchant_id
          AND m.product_id = CAST(p.product_id AS TEXT)
    )
  AND NOT EXISTS (
        SELECT 1
        FROM productcateg pc
        WHERE pc.merchant_id = p.merchant_id
          AND pc.merchant_categ_id = p.category
    );
