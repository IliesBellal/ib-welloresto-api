-- Reverts 178_payout_documents.up.sql. Supprime les factures de commission et
-- leurs compteurs : à ne jouer que sur un environnement de test (une fois des
-- factures réelles émises, elles doivent être conservées).
DROP TABLE IF EXISTS commission_invoices;
DROP TABLE IF EXISTS invoice_counters;
DROP TABLE IF EXISTS payout_documents;
