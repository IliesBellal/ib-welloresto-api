-- Rollback de 150_cds_drop_admin_pin.up.sql.
--
-- Recrée la colonne VIDE : les PIN supprimés par la migration montante ne sont
-- pas récupérables. Aucun code n'écrit ni ne lit cette colonne depuis le
-- retrait du PIN, le rollback ne restaure donc que le schéma.
ALTER TABLE cds_displays
    ADD COLUMN IF NOT EXISTS admin_pin_encrypted bytea;
