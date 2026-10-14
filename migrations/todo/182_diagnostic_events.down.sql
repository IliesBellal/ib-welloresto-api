-- Annule 182 : supprime le journal de diagnostic. Les événements sont perdus ;
-- le code continue de fonctionner (repli sur les logs applicatifs).
DROP TABLE IF EXISTS diagnostic_events;
