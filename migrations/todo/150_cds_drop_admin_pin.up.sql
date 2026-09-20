-- Retire le PIN administrateur des écrans CDS.
--
-- Ce PIN avait été porté du module kiosk en prévision d'un écran
-- d'administration local. Cet écran n'a jamais existé côté CDS : la
-- configuration se fait entièrement depuis le back-office. Le PIN ne
-- protégeait donc rien (ni l'application ni l'API ne le vérifiaient), il
-- était seulement stocké chiffré et affiché une fois à l'enrôlement.
--
-- Migration 147 déjà appliquée sur staging : on retire la colonne par une
-- nouvelle migration plutôt qu'en modifiant l'ancienne.
--
-- ⚠ Destructif : les PIN déjà générés sont perdus. Acceptable ici, puisqu'ils
-- ne donnaient accès à rien.
ALTER TABLE cds_displays
    DROP COLUMN IF EXISTS admin_pin_encrypted;
