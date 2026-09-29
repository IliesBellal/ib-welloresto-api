-- Reverts 161_menu_import_drafts.up.sql. À n'appliquer qu'après avoir
-- désactivé AI_TASK_MENU_OCR_ENABLED : les brouillons et crédits d'import
-- photo sont perdus (les photos R2 restent, sous menu-import/).
DROP TABLE IF EXISTS menu_import_ai_credits;
DROP TABLE IF EXISTS menu_import_drafts;
