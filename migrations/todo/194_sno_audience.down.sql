-- Rollback de 194_sno_audience.up.sql.
-- À jouer APRÈS le retrait du code qui écrit et lit ces tables (scannorder :
-- POST /scannorder/events et attribution des commandes ; analytics : onglet
-- ScanNOrder ; tâche CleanupOldAudienceEvents). La mesure d'audience est perdue.

DROP TABLE IF EXISTS sno_order_attributions;
DROP TABLE IF EXISTS sno_events;
