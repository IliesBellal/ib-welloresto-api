-- Rollback de 188_payments_collected_by.up.sql.
-- À jouer APRÈS le retrait du code qui lit ou écrit payments.collected_by
-- (delivery_sessions.FinalizeDeliveredStop, cash_registers, orders).
-- Perd l'encaisseur réel des paiements déjà livrés.

ALTER TABLE payments DROP COLUMN IF EXISTS collected_by;
