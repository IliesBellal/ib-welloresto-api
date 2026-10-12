-- Annule 181 : supprime la synchronisation comptable et son journal d'envois.
-- ATTENTION : le journal est la seule trace des jours déjà envoyés ; le supprimer
-- puis reconnecter un outil renverrait ces jours (doublons chez l'outil).
DROP TABLE IF EXISTS accounting_sync_items;
DROP TABLE IF EXISTS accounting_connections;
