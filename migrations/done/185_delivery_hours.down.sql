-- Rollback de 185_delivery_hours.up.sql.
-- À jouer APRÈS le retrait du code qui lit la table (CreateOrderSNO).

DROP TABLE IF EXISTS delivery_hours;
