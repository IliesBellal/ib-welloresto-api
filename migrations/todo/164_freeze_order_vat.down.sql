-- Reverts 164_freeze_order_vat.up.sql. À n'appliquer qu'après avoir retiré le
-- code qui lit/écrit ces colonnes (order_life_cycle.freezeOrderVAT et les
-- expressions models.OrderItemTVA*SQL / models.DeliveryFeesTVARateSQL) : la
-- TVA figée à la vente est perdue, les rapports retombent sur la configuration
-- produit du jour.
ALTER TABLE orderitems
    DROP COLUMN IF EXISTS tva_id,
    DROP COLUMN IF EXISTS tva_rate,
    DROP COLUMN IF EXISTS tva_reconstructed;

ALTER TABLE orders
    DROP COLUMN IF EXISTS delivery_fees_tva_rate,
    DROP COLUMN IF EXISTS delivery_fees_tva_reconstructed;
