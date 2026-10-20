-- Reverts 179_subscription_invoice_documents.up.sql. Supprime le journal des
-- factures d'abonnement envoyées : à ne jouer que sur un environnement de test.
DROP TABLE IF EXISTS subscription_invoice_documents;
