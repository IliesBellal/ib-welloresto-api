-- Mapping des groupes d'options Uber Eats : configurable_attribute_id passe de
-- integer à varchar(64). Voir docs/marketplace-fiabilite-import.md (lot 3).
--
-- configurable_attributes.id est un varchar : les groupes créés depuis portent un
-- identifiant texte (« attribute-<uuid> », « attr-<uuid> »), alors que cette colonne
-- ne reçoit que des entiers. CreateAttributeMapping échouait donc en 22P02 pour
-- tout groupe d'options jamais vu, et la commande Uber Eats qui le contenait était
-- perdue. Les anciens groupes (identifiants numériques) restent valides : la
-- conversion en texte est sans perte.
--
-- ORDRE DE DÉPLOIEMENT : indifférent. Avant la migration, un groupe inconnu passe en
-- texte dans le commentaire de la ligne (la commande est créée) ; après, il est créé
-- et mappé normalement. Rejouable.

DO $$
BEGIN
  IF (SELECT data_type FROM information_schema.columns
      WHERE table_name = 'integration_uber_eats_attributes_mapping'
        AND column_name = 'configurable_attribute_id') = 'integer' THEN
    ALTER TABLE integration_uber_eats_attributes_mapping
      ALTER COLUMN configurable_attribute_id TYPE varchar(64) USING configurable_attribute_id::text;
  END IF;
END $$;
