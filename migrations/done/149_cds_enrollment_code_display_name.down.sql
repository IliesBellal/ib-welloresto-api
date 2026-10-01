-- Rollback de 149_cds_enrollment_code_display_name.up.sql.
-- Sans effet sur cds_displays.name : les écrans déjà enrôlés gardent le nom
-- qui leur a été donné.

ALTER TABLE cds_enrollment_codes
    DROP COLUMN IF EXISTS display_name;
