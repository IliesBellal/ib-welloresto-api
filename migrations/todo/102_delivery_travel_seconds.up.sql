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
ALTER TABLE orders
  ADD COLUMN delivery_travel_seconds INT NULL DEFAULT NULL COMMENT 'in seconds' AFTER estimated_ready;

-- Moyenne glissante par marchand du temps de trajet livraison, calculée par
-- le cron UpdateAverageDeliveryTime (internal/tasks/delivery_time.go) toutes
-- les 15 minutes sur une fenêtre de 24h, à partir des orders.delivery_travel_seconds
-- déjà capturés. Sert de filet de sécurité quand un client n'a pas pu fournir
-- de valeur live, et d'estimation affichée avant checkout sur ScanNOrder
-- (avant que l'adresse ne permette un appel OSRM). Même forme que
-- average_distribution_time (merchant_id en clé primaire, une ligne par
-- marchand, écrasée à chaque recalcul) ; merchant_id en VARCHAR(64), comme
-- production_profiles (migrations/todo/101_production_profiles.up.sql) —
-- convention des tables récentes de ce projet.
CREATE TABLE average_delivery_time (
    merchant_id            VARCHAR(64) NOT NULL,
    delivery_time_seconds  INT         NOT NULL COMMENT 'in seconds',
    created_at              DATETIME   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              DATETIME   NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (merchant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
