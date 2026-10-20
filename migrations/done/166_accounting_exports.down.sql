-- Reverts 166_accounting_exports.up.sql. À n'appliquer qu'après avoir retiré le
-- code qui écrit / lit cette table (export comptable, liste et téléchargement
-- des exports) : les références aux PDF archivés sont perdues (les fichiers
-- restent dans le bucket R2 privé).
DROP TABLE IF EXISTS accounting_exports;
