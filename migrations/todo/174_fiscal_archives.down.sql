-- Reverts 174_fiscal_archives.up.sql. Supprime le journal des archives
-- générées : à ne jouer que sur un environnement de test.
DROP TABLE IF EXISTS fiscal_archives;
