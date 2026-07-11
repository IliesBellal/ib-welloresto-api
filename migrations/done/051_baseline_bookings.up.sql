-- ============================================================================
-- 051 — Baseline réservation (héritage PHP) : bookings, bookings_settings,
-- hours_of_operation.
--
-- ⚠️ DDL INFÉRÉ des requêtes Go du repo et des audits (audit-reservation-existant.md
-- §1.2) : ces tables préexistent en production (créées par l'ancien backend PHP,
-- aucun DDL dans le repo). AVANT application en production, vérifier chaque
-- définition contre `SHOW CREATE TABLE` prod ; sur une base où les tables
-- existent déjà, les CREATE TABLE IF NOT EXISTS sont des no-op et seuls les
-- index en fin de fichier s'appliquent.
--
-- Pas de FOREIGN KEY vers les tables legacy (merchant, orders, customer) :
-- leurs types exacts ne sont pas garantis ici (même convention que la 041).
-- Les statuts restent le vocabulaire legacy (PENDING_APPROVAL, ACCEPTED,
-- DENIED, CANCELED, ORDER_OPEN, '0') — la normalisation est la migration 053
-- (Phase 1, T-08).
-- ============================================================================

CREATE TABLE IF NOT EXISTS bookings (
    booking_id         INT AUTO_INCREMENT PRIMARY KEY,
    booking_number     VARCHAR(6) NULL,        -- généré côté Go (crypto), absent sur le flux public défectueux ; UNIQUE (merchant_id, booking_number) prévu en 053
    merchant_id        VARCHAR(64) NOT NULL,
    customer_id        VARCHAR(64) NULL,
    order_id           BIGINT UNSIGNED NULL,   -- lien caisse, détaché à la clôture (order_life_cycle.ClearBookings)
    booking_date_from  DATETIME NOT NULL,      -- heure locale marchand aujourd'hui ; bascule UTC = migration 056 (Lot 1 Phase 1+, addendum §7.7)
    booking_date_to    DATETIME NULL,
    booking_duration   INT NULL,               -- minutes, renseigné par le flux staff seulement
    party_size         INT NOT NULL DEFAULT 0,
    status             VARCHAR(32) NOT NULL,
    sequence_number    INT NOT NULL DEFAULT 0, -- compteur de modifications client
    comment            TEXT NULL,
    creation_date      DATETIME NULL,          -- UTC ; NOT NULL DEFAULT prévu en 053
    created_by         VARCHAR(64) NULL,       -- user_id ou littéral legacy 'WR_ONLINE_BOOKING' (migre vers source='web' en 053)
    deletion_date      DATETIME NULL,          -- code mort côté Go, conservé pour les données historiques
    deletion_reason_id VARCHAR(64) NULL,
    cancelled_by       VARCHAR(64) NULL        -- addendum §7.9 : SYSTEM | CUSTOMER | user_id staff (rempli à denied/cancelled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS bookings_settings (
    merchant_id                       VARCHAR(64) NOT NULL PRIMARY KEY,
    code                              VARCHAR(64) NULL,          -- slug public /rsv/{slug}
    enabled                           TINYINT(1) NOT NULL DEFAULT 0,
    default_booking_duration          INT NOT NULL DEFAULT 90,   -- minutes
    slot_interval_minutes             INT NOT NULL DEFAULT 15,
    auto_accept_reserve_bookings      TINYINT(1) NOT NULL DEFAULT 0,
    reserve_maximum_party_size        INT NOT NULL DEFAULT 8,
    first_booking_offset_minutes      INT NOT NULL DEFAULT 0,    -- lu mais jamais exploité (audit) ; retrait du code prévu au Lot 1 Phase 1
    last_booking_offset_minutes       INT NOT NULL DEFAULT 60,
    cancelable_by_customer            TINYINT(1) NOT NULL DEFAULT 1,
    cancel_booking_limit_offset_hours INT NOT NULL DEFAULT 2
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS hours_of_operation (
    id                 INT AUTO_INCREMENT PRIMARY KEY,
    merchant_id        VARCHAR(64) NOT NULL,
    day_of_week_from   TINYINT NOT NULL,       -- 1 = lundi … 7 = dimanche
    day_of_week_to     TINYINT NOT NULL,
    hour_from          TIME NOT NULL,          -- heure locale de service
    hour_to            TIME NOT NULL,
    booking_capacity   INT NOT NULL DEFAULT 0, -- couverts réservables sur la plage
    first_booking_time TIME NULL,
    last_booking_time  TIME NULL,
    valid_from         DATETIME NULL,          -- fenêtre de validité saisonnière
    valid_to           DATETIME NULL,
    enabled            TINYINT(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Index requis par les requêtes de disponibilité et de recherche (cadrage §1.4).
-- Appliqués séparément pour couvrir aussi les bases où les tables préexistent
-- (CREATE TABLE no-op ci-dessus). NB : la requête de dispo actuelle filtre sur
-- CAST(booking_date_from AS DATE), qui n'utilise pas idx_bookings_merchant_date ;
-- sa réécriture en bornes [date 00:00, date+1 00:00) appartient à T-10 (Phase 1).
ALTER TABLE bookings
    ADD INDEX IF NOT EXISTS idx_bookings_merchant_date (merchant_id, booking_date_from),
    ADD INDEX IF NOT EXISTS idx_bookings_merchant_status (merchant_id, status);

ALTER TABLE hours_of_operation
    ADD INDEX IF NOT EXISTS idx_hoo_merchant_day (merchant_id, day_of_week_from, enabled);
