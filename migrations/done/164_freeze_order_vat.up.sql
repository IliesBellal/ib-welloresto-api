-- TVA figée sur chaque ligne de commande (docs/TVA_FIGEE_LIGNES_COMMANDE.md).
--
-- Jusqu'ici le taux d'une ligne était retrouvé à la lecture via le produit
-- (tva_in_id / tva_take_away_id / tva_delivery_id selon orders.order_type) :
-- changer la catégorie d'un produit modifiait tous les anciens rapports. On
-- conserve désormais sur la ligne la catégorie et le taux appliqués à la vente,
-- et sur la commande le taux des frais de livraison (catégorie globale -1).
--
-- ATTENTION - Ce fichier contient un bloc DO qui fait des COMMIT entre les
-- lots du rattrapage : il doit être joué HORS bloc transactionnel (pas de
-- `psql -1`, pas de BEGIN englobant, autocommit actif dans un client
-- graphique) — sinon Postgres refuse avec « invalid transaction termination ».
-- Les lots limitent la durée des verrous de ligne : une ligne de commande
-- ouverte n'est bloquée que le temps de son propre lot.
--
-- ORDRE DE DÉPLOIEMENT : appliquer AVANT de déployer le code (le code écrit
-- ces colonnes). Les lignes créées entre la migration et le déploiement
-- restent vides : les lectures retombent alors sur le produit, comme avant.
-- Le fichier entier est rejouable à l'identique après le déploiement pour les
-- compléter : le rattrapage (sections 2 et 3) ne touche que les valeurs encore
-- vides.
-- Lancer de préférence hors service (orderitems est la plus grosse table).

-- 1. Colonnes. ADD COLUMN avec DEFAULT constant : instantané (pas de
-- réécriture de table) depuis Postgres 11.
ALTER TABLE orderitems
    ADD COLUMN IF NOT EXISTS tva_id integer NULL,
    ADD COLUMN IF NOT EXISTS tva_rate real NULL,
    ADD COLUMN IF NOT EXISTS tva_reconstructed boolean NOT NULL DEFAULT false;

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS delivery_fees_tva_rate real NULL,
    ADD COLUMN IF NOT EXISTS delivery_fees_tva_reconstructed boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN orderitems.tva_id IS 'Catégorie de TVA (tva_categories.tva_id) appliquée à la ligne, figée à l''écriture tant que la commande est ouverte (order_life_cycle.freezeOrderVAT). NULL = inconnue : les lectures retombent sur le produit.';
COMMENT ON COLUMN orderitems.tva_rate IS 'Taux de TVA (%) appliqué à la ligne et à ses suppléments, figé avec tva_id.';
COMMENT ON COLUMN orderitems.tva_reconstructed IS 'true = valeur rétro-remplie par la migration 164 depuis la configuration produit du jour de la migration, pas celle du jour de la vente.';
COMMENT ON COLUMN orders.delivery_fees_tva_rate IS 'Taux de TVA (%) des frais de livraison (catégorie -1), figé à l''écriture tant que la commande est ouverte.';
COMMENT ON COLUMN orders.delivery_fees_tva_reconstructed IS 'true = valeur rétro-remplie par la migration 164 depuis le taux du jour de la migration.';

-- 2. Rattrapage des lignes, par lots de 20 000 order_item_id. Même dérivation
-- que les rapports (order_type -> catégorie du produit). Les lignes dont le
-- produit ou la catégorie est introuvable restent NULL.
DO $$
DECLARE
    batch_size constant integer := 20000;
    batch_start integer := 0;
    max_id integer;
BEGIN
    SELECT COALESCE(MAX(order_item_id), 0) INTO max_id FROM orderitems;
    WHILE batch_start <= max_id LOOP
        UPDATE orderitems oi
        SET tva_id = tc.tva_id,
            tva_rate = tc.tva_rate,
            tva_reconstructed = true
        FROM orders o, products p, tva_categories tc
        WHERE oi.order_item_id > batch_start
          AND oi.order_item_id <= batch_start + batch_size
          AND oi.tva_id IS NULL
          AND o.order_id = oi.order_id
          AND p.product_id = oi.product_id
          AND tc.tva_id = CASE
                  WHEN o.order_type = 'DELIVERY' THEN p.tva_delivery_id
                  WHEN o.order_type = 'TAKE_AWAY' THEN p.tva_take_away_id
                  ELSE p.tva_in_id
              END;
        COMMIT;
        batch_start := batch_start + batch_size;
    END LOOP;
END $$;

-- 3. Rattrapage du taux des frais de livraison, par lots de 20 000 order_id,
-- limité aux commandes qui en portent (inutile de réécrire toutes les
-- commandes pour un taux qui multiplie 0).
DO $$
DECLARE
    batch_size constant integer := 20000;
    batch_start integer := 0;
    max_id integer;
    fees_rate real;
BEGIN
    SELECT tva_rate INTO fees_rate FROM tva_categories WHERE tva_id = -1;
    IF fees_rate IS NULL THEN
        RETURN;
    END IF;
    SELECT COALESCE(MAX(order_id), 0) INTO max_id FROM orders;
    WHILE batch_start <= max_id LOOP
        UPDATE orders
        SET delivery_fees_tva_rate = fees_rate,
            delivery_fees_tva_reconstructed = true
        WHERE order_id > batch_start
          AND order_id <= batch_start + batch_size
          AND delivery_fees_tva_rate IS NULL
          AND delivery_fees <> 0;
        COMMIT;
        batch_start := batch_start + batch_size;
    END LOOP;
END $$;
