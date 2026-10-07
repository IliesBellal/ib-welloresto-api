-- Reverts 165_cash_register_closing_modes.up.sql. À n'appliquer qu'après avoir
-- retiré le code qui lit/écrit ces objets (cash_registers/closing_mode.go,
-- ouverture et fermeture de registre, export comptable) : l'historique des
-- modes de clôture et le mode inscrit sur chaque registre sont perdus.
ALTER TABLE cash_registers DROP CONSTRAINT IF EXISTS chk_cash_registers_closing_mode;
ALTER TABLE cash_registers DROP COLUMN IF EXISTS closing_mode;
DROP TABLE IF EXISTS merchant_closing_modes;
