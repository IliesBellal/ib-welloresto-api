-- Rollback de 194_franchise_hub_tracking.up.sql.
-- À jouer APRÈS le retrait du code qui écrit et lit ces tables (scannorder :
-- événements et attribution du hub ; analytics : onglet ScanNOrder). Les
-- statistiques du hub sont perdues.

DROP TABLE IF EXISTS franchise_hub_orders;
DROP TABLE IF EXISTS franchise_hub_events;
