-- PROMPT 21 — Assainir le modèle de remises, contraction préparée.
--
-- NOT APPLIED as part of this lot (explicit instruction — prepared only,
-- same convention as 117_cleanup_deletion_reason_id_quotes).
--
-- Drops orders.cart_discount_id / cart_discount_code: superseded by
-- discount_redemptions (scope=CART rows can reference several discounts per
-- order, which a single cart_discount_id column structurally cannot — see
-- docs/decisions.md PROMPT 21). Confirmed dead in Phase 1: no Go code reads
-- or writes either column (internal/models DTOs carry CartDiscountID/Code
-- fields but no repository ever selects/inserts them), and no frontend
-- consumes them either.
--
-- orders.cart_discount_amount is NOT dropped here — it stays, and Phase 4
-- (deferred: no cart-discount application logic exists yet anywhere in the
-- codebase to feed it) is expected to keep populating it once built.
--
-- To play once no deployed code references these columns any more (verify
-- again at that time — this lot only confirms the state as of 2026-09-05).

ALTER TABLE orders
    DROP COLUMN IF EXISTS cart_discount_id,
    DROP COLUMN IF EXISTS cart_discount_code;
