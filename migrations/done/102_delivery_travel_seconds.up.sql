-- Temps de trajet livraison (Google Maps côté POS, OSRM côté ScanNOrder),
-- capturé au moment où le client renseigne l'adresse de livraison. Permet à
-- la production de calculer la vraie deadline cuisine
-- (estimated_ready - delivery_travel_seconds) au lieu d'utiliser
-- estimated_ready seul, qui est en réalité la date de livraison promise au
-- client, pas la date de fin de cuisine.
--
-- Nullable : NULL pour les commandes non-livraison, ou tant qu'aucune valeur
-- (live ou moyenne de repli, cf. resolveDeliveryTravelSeconds côté
-- order_life_cycle/repository.go) n'a pu être déterminée.
--
-- PostgreSQL migration (réécrite le 2026-09-07, PROMPT 27 Phase 1) : le
-- fichier original était en syntaxe MySQL non convertie (COMMENT inline,
-- AFTER, ENGINE=InnoDB) — invalide en Postgres. Cette version reprend le
-- schéma tel que réellement obtenu sur staging
-- (docs/migration-postgres/67-migration-status-audit.md §1.2) : orders est
-- une table déjà large en production, ADD COLUMN nullable sans DEFAULT reste
-- une opération de métadonnées pure (pas de réécriture de table sous
-- Postgres 11+), donc sans risque de verrou long malgré le volume.
ALTER TABLE orders
  ADD COLUMN IF NOT EXISTS delivery_travel_seconds integer;

COMMENT ON COLUMN orders.delivery_travel_seconds IS 'Temps de trajet livraison en secondes, capturé à la saisie de l''adresse. NULL pour les commandes non-livraison ou tant qu''aucune valeur (live ou moyenne de repli) n''a pu être déterminée.';

-- Moyenne glissante par marchand du temps de trajet livraison, calculée par
-- le cron UpdateAverageDeliveryTime (internal/tasks/delivery_time.go) toutes
-- les 15 minutes sur une fenêtre de 24h, à partir des orders.delivery_travel_seconds
-- déjà capturés. Sert de filet de sécurité quand un client n'a pas pu fournir
-- de valeur live, et d'estimation affichée avant checkout sur ScanNOrder
-- (avant que l'adresse ne permette un appel OSRM). Même forme que
-- average_distribution_time (merchant_id en clé primaire, une ligne par
-- marchand, écrasée à chaque recalcul) ; merchant_id en varchar(64), comme
-- production_profiles (migrations/todo/101_production_profiles.up.sql) —
-- convention des tables récentes de ce projet. updated_at NOT NULL DEFAULT
-- CURRENT_TIMESTAMP sans trigger (comme production_profiles) : pas de reprise
-- du comportement ON UPDATE CURRENT_TIMESTAMP de MySQL, le cron réécrit la
-- ligne entière à chaque recalcul (UPSERT), donc updated_at est fourni
-- explicitement par le code applicatif, jamais par la base.
CREATE TABLE average_delivery_time (
    merchant_id            varchar(64) NOT NULL,
    delivery_time_seconds  integer     NOT NULL,
    created_at              timestamp  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              timestamp  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (merchant_id)
);

COMMENT ON COLUMN average_delivery_time.delivery_time_seconds IS 'Moyenne glissante en secondes, fenêtre 24h, recalculée toutes les 15 minutes par UpdateAverageDeliveryTime.';
