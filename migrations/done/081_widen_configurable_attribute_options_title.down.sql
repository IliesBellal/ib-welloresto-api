-- Rollback de 081_widen_configurable_attribute_options_title.up.sql
--
-- ATTENTION : ce down ECHOUERA (erreur 22001, "value too long for type
-- character varying(25)") si au moins un libelle stocke depasse 25 caracteres -
-- ce qui sera le cas des qu'un import aura ecrit des options aux libelles longs.
-- Contrairement a MySQL non-strict, Postgres ne tronque pas silencieusement.
--
-- Verifier avant de jouer ce rollback :
--
--   SELECT id, configurable_attribute_id, title, length(title)
--   FROM configurable_attribute_options
--   WHERE length(title) > 25
--   ORDER BY length(title) DESC;
--
-- Si la requete remonte des lignes, il faut arbitrer explicitement (raccourcir
-- les libelles concernes, ou renoncer au rollback). Aucune troncature
-- automatique n'est faite ici : elle ferait perdre de la donnee metier sans
-- trace.

ALTER TABLE configurable_attribute_options
    ALTER COLUMN title TYPE varchar(25);

COMMENT ON COLUMN configurable_attribute_options.title IS NULL;
