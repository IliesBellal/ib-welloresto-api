-- Retire la banque d'images. Les fichiers R2 du dossier image_bank/ restent
-- en place (des établissements peuvent encore référencer leurs URLs).
DROP TABLE IF EXISTS image_bank;
