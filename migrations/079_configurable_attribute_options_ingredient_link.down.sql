-- Rollback de 079_configurable_attribute_options_ingredient_link.up.sql
ALTER TABLE configurable_attribute_options
    DROP COLUMN IF EXISTS component_id,
    DROP COLUMN IF EXISTS quantity,
    DROP COLUMN IF EXISTS unit_of_measure;
