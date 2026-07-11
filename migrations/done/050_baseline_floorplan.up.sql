-- ============================================================================
-- 050 — Baseline plan de salle (héritage PHP) : floors, locations, floor_areas,
-- order_location, booked_location.
--
-- ⚠️ DDL INFÉRÉ des requêtes Go du repo et des audits (audit-tables-plan-de-salle.md
-- §1.1) : ces tables préexistent en production (créées par l'ancien backend PHP,
-- aucun DDL dans le repo). AVANT application en production, vérifier chaque
-- définition contre `SHOW CREATE TABLE` prod ; sur une base où les tables
-- existent déjà, les CREATE TABLE IF NOT EXISTS sont des no-op et seuls les
-- index en fin de fichier s'appliquent.
--
-- Pas de FOREIGN KEY vers les tables legacy (merchant, orders, customer) :
-- leurs types exacts ne sont pas garantis ici (même convention que la 041).
-- ============================================================================

CREATE TABLE IF NOT EXISTS floors (
    id          INT AUTO_INCREMENT PRIMARY KEY,
    merchant_id VARCHAR(64) NOT NULL,
    name        VARCHAR(255) NOT NULL,
    enabled     TINYINT(1) NOT NULL DEFAULT 1,
    KEY idx_floors_merchant (merchant_id, enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS locations (
    location_id    INT AUTO_INCREMENT PRIMARY KEY,
    merchant_id    VARCHAR(64) NOT NULL,
    floor_id       INT NULL,
    location_name  VARCHAR(255) NOT NULL,
    location_desc  VARCHAR(255) NULL,          -- legacy : lu partout, éditable nulle part (statu quo Lot 1)
    seats          INT NOT NULL DEFAULT 0,
    location_order INT NOT NULL DEFAULT 0,
    shape          VARCHAR(16) NOT NULL DEFAULT 'square',  -- circle | square | rectangle
    current_x      FLOAT NOT NULL DEFAULT 0,   -- canvas virtuel 1000x1000, sans unité réelle
    current_y      FLOAT NOT NULL DEFAULT 0,
    current_width  FLOAT NOT NULL DEFAULT 0,
    current_height FLOAT NOT NULL DEFAULT 0,
    angle          FLOAT NOT NULL DEFAULT 0,   -- degrés 0-359
    enabled        TINYINT(1) NOT NULL DEFAULT 1,
    KEY idx_locations_merchant (merchant_id, enabled),
    KEY idx_locations_floor (floor_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS floor_areas (
    id           INT AUTO_INCREMENT PRIMARY KEY,
    floor_id     INT NOT NULL,
    name         VARCHAR(255) NULL,
    points       JSON NULL,                    -- polygone dessiné (calque décoratif, gelé au Lot 1)
    x            FLOAT NOT NULL DEFAULT 0,
    y            FLOAT NOT NULL DEFAULT 0,
    angle        FLOAT NOT NULL DEFAULT 0,
    stroke_color VARCHAR(32) NULL,
    color        VARCHAR(32) NULL,
    enabled      TINYINT(1) NOT NULL DEFAULT 1,
    KEY idx_floor_areas_floor (floor_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS order_location (
    order_id    BIGINT UNSIGNED NOT NULL,
    location_id INT NOT NULL,
    KEY idx_order_location_order (order_id),
    KEY idx_order_location_location (location_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS booked_location (
    booking_id  INT NOT NULL,
    location_id INT NOT NULL,
    KEY idx_booked_location_booking (booking_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Index requis par le contrôle de conflit table × créneau (T-07) et la vue plan
-- de salle. Appliqué séparément pour couvrir aussi les bases où booked_location
-- préexiste (CREATE TABLE no-op ci-dessus).
ALTER TABLE booked_location
    ADD INDEX IF NOT EXISTS idx_booked_location_location (location_id);
