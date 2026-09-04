-- Rollback de 114_write_path_instrumentation.up.sql
--
-- Ne restaure pas la casse d'origine de brand_status (36 lignes normalisées
-- en majuscules) : aucune trace de la casse d'origine n'est conservée, et la
-- ré-abaisser en minuscules réintroduirait le bug que ce lot corrige. Les
-- valeurs de rétro-remplissage (order_source, cancelled_by_type) disparaissent
-- simplement avec les colonnes.

ALTER TABLE orders DROP CONSTRAINT IF EXISTS chk_orders_cancelled_by_type;
ALTER TABLE orders DROP COLUMN IF EXISTS cancelled_by_type;

ALTER TABLE customer DROP COLUMN IF EXISTS acquisition_source;

ALTER TABLE orders DROP CONSTRAINT IF EXISTS chk_orders_order_source;
ALTER TABLE orders DROP COLUMN IF EXISTS order_source;

ALTER TABLE orderitems DROP CONSTRAINT IF EXISTS chk_orderitems_cost_price_reason;
ALTER TABLE orderitems DROP COLUMN IF EXISTS cost_price_reason;
ALTER TABLE orderitems DROP COLUMN IF EXISTS cost_price_unit;
