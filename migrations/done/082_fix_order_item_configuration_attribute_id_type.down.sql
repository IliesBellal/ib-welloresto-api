-- Rollback de 082_fix_order_item_configuration_attribute_id_type.up.sql
--
-- ATTENTION : ce down ECHOUERA (erreur 22P02, "invalid input syntax for type
-- integer") des que la colonne contient une valeur non numerique — ce qui
-- sera systematiquement le cas pour toute ligne inseree apres le up (IDs
-- prefixes 'attribute-<uuid>', desormais acceptes sans erreur). Contrairement
-- au sens up (integer -> varchar, toujours valide), le sens varchar ->
-- integer n'est valide que pour les anciennes lignes "garbage" a '0'.
--
-- Verifier avant de jouer ce rollback :
--
--   SELECT id, order_item_id, configuration_attribute_id
--   FROM order_item_configuration
--   WHERE configuration_attribute_id !~ '^[0-9]+$'
--   ORDER BY id DESC
--   LIMIT 20;
--
-- Si la requete remonte des lignes, revenir a integer ferait a nouveau
-- echouer la creation/mise a jour de commandes avec configuration — c'est
-- exactement le bug que le up corrige. Ne pas rollback en production sans
-- arbitrage explicite.

ALTER TABLE order_item_configuration
    ALTER COLUMN configuration_attribute_id TYPE integer
    USING configuration_attribute_id::integer;

COMMENT ON COLUMN order_item_configuration.configuration_attribute_id IS NULL;
