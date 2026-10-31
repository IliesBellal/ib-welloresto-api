-- Retour à integer. Impossible tant qu'un groupe a un identifiant texte (« attr-… ») :
-- le ::integer échoue alors, sans rien modifier. Les mappings de ces groupes doivent
-- d'abord être supprimés (DELETE FROM integration_uber_eats_attributes_mapping
-- WHERE configurable_attribute_id !~ '^[0-9]+$'), ce qui renvoie leurs options en
-- texte dans les commandes à venir.

ALTER TABLE integration_uber_eats_attributes_mapping
  ALTER COLUMN configurable_attribute_id TYPE integer USING configurable_attribute_id::integer;
