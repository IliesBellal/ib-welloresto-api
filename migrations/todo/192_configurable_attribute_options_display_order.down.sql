-- Reverts 192_configurable_attribute_options_display_order.up.sql.
--
-- ORDRE : déployer l'ancien code AVANT ce rollback (le code courant sélectionne
-- display_order). L'ordre saisi par les restaurateurs est perdu.
ALTER TABLE configurable_attribute_options
    DROP COLUMN IF EXISTS display_order;
