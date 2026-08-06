-- PostgreSQL migration : lien optionnel ingredient (composant de stock) sur
-- une option d'attribut configurable ("Options & Supplements" back-office,
-- ex. option "Grande" de l'attribut "Taille Pizza" liee au composant
-- "Pate a pizza 300g").
--
-- Permet de projeter un cout de revient par option (UI back-office deja
-- cablee, cf. wello-back-office/src/pages/Attributes.tsx) sans encore
-- brancher la deduction de stock a la commande (ConsumeOrderStock,
-- internal/modules/stocks/repository.go) -- chantier separe, non traite ici.
--
-- Les 3 colonnes sont NULL-ables : la grande majorite des options n'ont pas
-- d'ingredient lie (ex. "Sans glacons"). NULL = aucun ingredient.
--
-- Nommage aligne sur la table soeur `requires` (recipe_id -> component_id +
-- quantity + unit_of_measure), qui relie deja un composant a une quantite et
-- une unite, mais au niveau recette plutot qu'option.
--
-- Aucune FK creee (convention du chantier de migration, cf.
-- docs/migration-postgres/04-schema-postgres-target.sql).
-- FK candidates (non creees) :
--   component_id -> components.component_id
--   unit_of_measure -> unit_of_measure.id

ALTER TABLE configurable_attribute_options
    ADD COLUMN IF NOT EXISTS component_id integer,
    ADD COLUMN IF NOT EXISTS quantity double precision,
    ADD COLUMN IF NOT EXISTS unit_of_measure integer;

COMMENT ON COLUMN configurable_attribute_options.component_id IS 'Ingredient (components.component_id) lie a cette option, pour projection de cout. NULL = aucun ingredient. Pas de FK (convention du depot).';
COMMENT ON COLUMN configurable_attribute_options.quantity IS 'Quantite de l''ingredient consommee par selection de cette option, dans l''unite unit_of_measure. NULL si component_id est NULL.';
COMMENT ON COLUMN configurable_attribute_options.unit_of_measure IS 'Unite (unit_of_measure.id) de la quantite ci-dessus. NULL si component_id est NULL.';
