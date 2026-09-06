-- PROMPT 21 — Assainir le modèle de remises, Phase 2+3 (expansion).
--
-- Additif uniquement, aucune colonne supprimée (voir 119_drop_cart_discount_
-- legacy_columns pour les DROP préparés, non joués par ce lot). Trois volets :
--
--   1. discounts.discount_id_new : nouvelle clé entière, attribuée aux
--      remises existantes SANS jamais réécrire orderitems.discount_id — les
--      remises déjà au format numérique (discount_id = '3', '4', ... '11')
--      reçoivent la même valeur en entier, donc les 4592 lignes orderitems
--      qui les référencent déjà restent valides sans aucune migration de
--      données. Les remises au format "discount-<uuid>" (Sprint 2, jamais
--      référencées par orderitems.discount_id — structurellement impossible,
--      colonne integer) reçoivent des valeurs neuves attribuées après le
--      plus grand entier déjà en circulation (côté discounts ET côté
--      orderitems, y compris les entrées orphelines) pour ne jamais entrer
--      en collision avec une valeur historique, même orpheline.
--      discounts.legacy_discount_id conserve l'ancien identifiant varchar,
--      jamais supprimée : sert de diagnostic si quelque chose tourne mal.
--
--   2. discounts_products / discounts_schedules / discounts_products_options :
--      même traitement (discount_id_new backfillé par jointure), avec de
--      vraies contraintes FOREIGN KEY vers discounts(discount_id_new) —
--      aucune des trois n'en a jamais eu (rapport 57 : le mismatch de type
--      varchar/integer l'en empêchait structurellement). Les trois tables
--      sont à 0 ligne orpheline (vérifié sur staging), la FK est donc
--      validée directement, pas en NOT VALID.
--      Pas de FK sur orderitems.discount_id : 17 lignes orphelines connues
--      (discount_id 81/83/84/89/90, remises supprimées) la feraient échouer.
--      Laissées telles quelles, comme actées en Phase 1 — dette documentée,
--      pas oubliée.
--
--   3. discount_redemptions (créée à vide par 041_cart_discounts, jamais
--      câblée côté Go — rapport 57) : étendue pour servir de table de
--      liaison commande×remise au grain ligne ET panier.
--        - discount_id passe de varchar(64) à integer (0 ligne existante,
--          changement de type sans risque) + FK vers discounts(discount_id_new).
--        - order_item_id (nouveau, nullable) : NOT NULL seulement pour une
--          remise de portée PRODUCT_LINE ; FK vers orderitems(order_item_id).
--        - customer_id : varchar(64) -> integer, corrige au passage le
--          deuxième mismatch de type noté rapport 57 (customer.customer_id
--          cible est integer) ; FK ajoutée.
--        - scope (PRODUCT_LINE | CART) : distinct de discounts.discount_scope
--          (qui décrit la configuration de la remise, pas une utilisation
--          précise) — nommé différemment pour ne pas confondre les deux.
--        - is_reconstructed : distingue une ligne déduite rétroactivement
--          (Phase 5, migration suivante) d'une ligne écrite en direct par le
--          code applicatif.
--        - L'ancienne UNIQUE (discount_id, order_id) est correcte pour une
--          remise panier (une seule par commande) mais fausse pour une
--          remise ligne (la même remise peut s'appliquer à 2 lignes
--          distinctes de la même commande) : remplacée par deux index
--          uniques partiels, un par portée.

-- ============================================================================
-- 1. discounts : nouvelle clé entière
-- ============================================================================
ALTER TABLE discounts
    ADD COLUMN IF NOT EXISTS legacy_discount_id varchar(50),
    ADD COLUMN IF NOT EXISTS discount_id_new integer;

COMMENT ON COLUMN discounts.legacy_discount_id IS 'Ancien discount_id (varchar) conservé pour diagnostic après la bascule vers discount_id_new (PROMPT 21). Jamais supprimée, même une fois discount_id_new devenu la clé de référence.';
COMMENT ON COLUMN discounts.discount_id_new IS 'Nouvelle clé entière (PROMPT 21) : identique à l''ancien discount_id pour les remises déjà au format numérique (pas de réécriture d''orderitems.discount_id nécessaire), attribuée après coup pour les remises "discount-<uuid>" (Sprint 2). Devient la clé de référence pour tout nouveau code ; la bascule de la PRIMARY KEY elle-même est différée à un lot de contraction ultérieur.';

UPDATE discounts
SET legacy_discount_id = discount_id
WHERE legacy_discount_id IS NULL;

-- 1.a Remises déjà au format numérique : valeur inchangée, cast en entier.
UPDATE discounts
SET discount_id_new = discount_id::integer
WHERE discount_id_new IS NULL
  AND discount_id ~ '^[0-9]+$';

-- 1.b Remises "discount-<uuid>" : nouvelles valeurs attribuées après le plus
-- grand entier déjà en circulation, côté discounts (format numérique) ET
-- côté orderitems (y compris les valeurs orphelines, pour ne jamais leur
-- entrer en collision plus tard). Calculé dynamiquement : ce lot migre aussi
-- la production, dont les valeurs réelles diffèrent de celles observées sur
-- staging au moment de l'écriture de cette migration.
WITH base AS (
    SELECT GREATEST(
        COALESCE((SELECT MAX(discount_id::integer) FROM discounts WHERE discount_id ~ '^[0-9]+$'), 0),
        COALESCE((SELECT MAX(discount_id) FROM orderitems WHERE discount_id IS NOT NULL), 0)
    ) AS start_after
),
numbered AS (
    SELECT discount_id, row_number() OVER (ORDER BY creation_date, discount_id) AS rn
    FROM discounts
    WHERE discount_id_new IS NULL
      AND discount_id !~ '^[0-9]+$'
)
UPDATE discounts d
SET discount_id_new = numbered.rn + base.start_after
FROM numbered, base
WHERE d.discount_id = numbered.discount_id;

ALTER TABLE discounts ALTER COLUMN discount_id_new SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_discounts_discount_id_new ON discounts (discount_id_new);

-- Séquence pour toute nouvelle remise créée après ce lot : calée sur le plus
-- grand discount_id_new déjà attribué (jamais 0, discounts a toujours >= 1
-- ligne à ce stade du projet, mais COALESCE conservé par prudence).
CREATE SEQUENCE IF NOT EXISTS discounts_discount_id_new_seq OWNED BY discounts.discount_id_new;
SELECT setval('discounts_discount_id_new_seq', COALESCE((SELECT MAX(discount_id_new) FROM discounts), 0) + 1, false);
ALTER TABLE discounts ALTER COLUMN discount_id_new SET DEFAULT nextval('discounts_discount_id_new_seq');

-- ============================================================================
-- 2. Tables filles : discount_id_new backfillé + vraies FK (jamais eues)
-- ============================================================================
ALTER TABLE discounts_products ADD COLUMN IF NOT EXISTS discount_id_new integer;
UPDATE discounts_products dp
SET discount_id_new = d.discount_id_new
FROM discounts d
WHERE d.discount_id = dp.discount_id
  AND dp.discount_id_new IS NULL;
ALTER TABLE discounts_products ALTER COLUMN discount_id_new SET NOT NULL;
ALTER TABLE discounts_products
    ADD CONSTRAINT fk_discounts_products_discount_id_new
    FOREIGN KEY (discount_id_new) REFERENCES discounts (discount_id_new);

ALTER TABLE discounts_schedules ADD COLUMN IF NOT EXISTS discount_id_new integer;
UPDATE discounts_schedules ds
SET discount_id_new = d.discount_id_new
FROM discounts d
WHERE d.discount_id = ds.discount_id
  AND ds.discount_id_new IS NULL;
ALTER TABLE discounts_schedules ALTER COLUMN discount_id_new SET NOT NULL;
ALTER TABLE discounts_schedules
    ADD CONSTRAINT fk_discounts_schedules_discount_id_new
    FOREIGN KEY (discount_id_new) REFERENCES discounts (discount_id_new);

-- discounts_products_options.discount_id est varchar(20) : trop court pour un
-- "discount-<uuid>" (45 caractères). Aucune ligne moderne n'y a encore jamais
-- été écrite (vérifié : 0 ligne staging), donc aucun risque de troncature
-- rencontré à ce jour — mais le piège reste latent tant que discount_id_new
-- n'est pas la référence. Traité maintenant plutôt que redécouvert plus tard.
ALTER TABLE discounts_products_options ADD COLUMN IF NOT EXISTS discount_id_new integer;
UPDATE discounts_products_options dpo
SET discount_id_new = d.discount_id_new
FROM discounts d
WHERE d.discount_id = dpo.discount_id
  AND dpo.discount_id_new IS NULL;
ALTER TABLE discounts_products_options ALTER COLUMN discount_id_new SET NOT NULL;
ALTER TABLE discounts_products_options
    ADD CONSTRAINT fk_discounts_products_options_discount_id_new
    FOREIGN KEY (discount_id_new) REFERENCES discounts (discount_id_new);

-- ============================================================================
-- 3. discount_redemptions : extension en table de liaison commande×remise
-- ============================================================================
CREATE TYPE discount_redemptions_scope_enum AS ENUM ('PRODUCT_LINE', 'CART');

ALTER TABLE discount_redemptions
    ADD COLUMN IF NOT EXISTS scope discount_redemptions_scope_enum,
    ADD COLUMN IF NOT EXISTS order_item_id integer,
    ADD COLUMN IF NOT EXISTS is_reconstructed boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN discount_redemptions.scope IS 'Portée de CETTE utilisation (pas de la remise en général, voir discounts.discount_scope) : PRODUCT_LINE (order_item_id renseigné) ou CART (order_item_id NULL, une remise panier par commande).';
COMMENT ON COLUMN discount_redemptions.order_item_id IS 'Ligne de commande remisée. NULL uniquement pour scope=CART. Voir chk_discount_redemptions_scope_order_item.';
COMMENT ON COLUMN discount_redemptions.is_reconstructed IS 'true pour une ligne déduite rétroactivement de orderitems.base_price/price (PROMPT 21 Phase 5, migration 120) — pas observée en direct au moment de l''application de la remise. false pour toute ligne écrite par le code applicatif.';

-- discount_id : varchar(64) -> integer (0 ligne existante, changement de
-- type sans risque) pour matcher discounts.discount_id_new.
ALTER TABLE discount_redemptions ALTER COLUMN discount_id TYPE integer USING discount_id::integer;
ALTER TABLE discount_redemptions ALTER COLUMN discount_id DROP DEFAULT;

-- customer_id : varchar(64) -> integer, corrige le deuxième mismatch de type
-- noté rapport 57 (customer.customer_id cible est integer). 0 ligne existante.
ALTER TABLE discount_redemptions ALTER COLUMN customer_id TYPE integer USING customer_id::integer;

ALTER TABLE discount_redemptions
    ADD CONSTRAINT fk_discount_redemptions_discount_id
    FOREIGN KEY (discount_id) REFERENCES discounts (discount_id_new);
ALTER TABLE discount_redemptions
    ADD CONSTRAINT fk_discount_redemptions_order_id
    FOREIGN KEY (order_id) REFERENCES orders (order_id);

-- orderitems' PRIMARY KEY is composite (order_item_id, order_id, product_id)
-- — order_item_id alone has no unique constraint to reference yet, even
-- though it's already unique in practice (GENERATED ALWAYS AS IDENTITY).
-- Made explicit here so the FK below has something to point at.
CREATE UNIQUE INDEX IF NOT EXISTS uq_orderitems_order_item_id ON orderitems (order_item_id);
ALTER TABLE discount_redemptions
    ADD CONSTRAINT fk_discount_redemptions_order_item_id
    FOREIGN KEY (order_item_id) REFERENCES orderitems (order_item_id);
ALTER TABLE discount_redemptions
    ADD CONSTRAINT fk_discount_redemptions_customer_id
    FOREIGN KEY (customer_id) REFERENCES customer (customer_id);

ALTER TABLE discount_redemptions
    ADD CONSTRAINT chk_discount_redemptions_scope_order_item
    CHECK (
        (scope = 'PRODUCT_LINE' AND order_item_id IS NOT NULL)
        OR (scope = 'CART' AND order_item_id IS NULL)
    );

-- Remplace l'ancienne UNIQUE (discount_id, order_id) : correcte seulement
-- pour une remise panier, fausse pour une remise ligne (une même remise
-- peut s'appliquer à 2 lignes distinctes de la même commande).
ALTER TABLE discount_redemptions DROP CONSTRAINT IF EXISTS uq_discount_order;
DROP INDEX IF EXISTS uq_discount_redemptions_uq_discount_order;
CREATE UNIQUE INDEX IF NOT EXISTS uq_discount_redemptions_product_line ON discount_redemptions (order_item_id) WHERE scope = 'PRODUCT_LINE';
CREATE UNIQUE INDEX IF NOT EXISTS uq_discount_redemptions_cart ON discount_redemptions (discount_id, order_id) WHERE scope = 'CART';

-- scope devient NOT NULL une fois les contraintes ci-dessus en place (aucune
-- ligne existante à ce stade — discount_redemptions est vide, rapport 57).
ALTER TABLE discount_redemptions ALTER COLUMN scope SET NOT NULL;
