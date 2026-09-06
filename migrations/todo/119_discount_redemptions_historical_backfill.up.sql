-- PROMPT 21 — Assainir le modèle de remises, Phase 5 (reprise de l'historique).
--
-- Reconstitue dans discount_redemptions les remises de portée ligne déjà
-- observables dans orderitems : base_price != price ET discount_id renseigné
-- ET pointant vers une remise qui existe encore (jointure sur
-- discounts.discount_id_new — le mismatch de type qui empêchait cette
-- jointure jusqu'ici est résolu par la migration 118). Les lignes dont le
-- discount_id est orphelin (17 lignes connues, remises supprimées depuis)
-- sont exclues par construction : rien à leur attribuer de façon fiable,
-- laissées telles quelles comme acté en Phase 1/2.
--
-- Pas de reprise possible pour les remises panier : cart_discount_amount n'a
-- jamais été alimenté (Phase 4, différée) — il n'y a rien à déduire, pas une
-- donnée difficile à retrouver.
--
-- customer_id laissé NULL : orderitems ne porte pas cette information
-- directement, et une jointure via orders.customer_id serait une déduction
-- de plus n'apportant rien à l'objectif (quelle remise coûte le plus, sur
-- quoi) — pas reconstitué plutôt que reconstitué sur une base fragile.
--
-- is_reconstructed = true sur toutes les lignes insérées ici : les distingue
-- en permanence d'une ligne écrite en direct par le code applicatif.
--
-- Idempotent : ON CONFLICT DO NOTHING sur l'index unique partiel
-- (order_item_id) WHERE scope = 'PRODUCT_LINE' posé par la migration 118.

INSERT INTO discount_redemptions (
    scope, discount_id, order_id, order_item_id, merchant_id, customer_id,
    amount_applied_cents, is_reconstructed, created_at
)
SELECT
    'PRODUCT_LINE',
    oi.discount_id,
    oi.order_id,
    oi.order_item_id,
    oi.merchant_id,
    NULL,
    oi.base_price - oi.price,
    true,
    oi.ordered_on
FROM orderitems oi
WHERE oi.base_price IS NOT NULL
  AND oi.base_price <> oi.price
  AND oi.discount_id IS NOT NULL
  AND EXISTS (SELECT 1 FROM discounts d WHERE d.discount_id_new = oi.discount_id)
ON CONFLICT DO NOTHING;
