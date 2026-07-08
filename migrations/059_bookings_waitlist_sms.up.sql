-- ============================================================================
-- 059 — Phase 3 Lot 1 : SMS bidirectionnel, liste d'attente, journal d'events.
--
-- Trois ajouts dans une seule migration :
--   1. Colonnes bookings_settings : sms_enabled + paramétrage liste d'attente.
--   2. Table booking_waitlist : file d'attente salle (walk-in staff + public).
--   3. Table booking_events : journal des événements de réservation/waitlist
--      (no-show, réattribution, reconfirmation/annulation SMS).
--
-- Conventions alignées sur les migrations 051/057 : DATETIME + UTC_TIMESTAMP,
-- ADD COLUMN IF NOT EXISTS idempotent, pas de FOREIGN KEY vers les tables
-- legacy (merchant, customer, bookings) dont les types ne sont pas garantis.
-- ============================================================================

ALTER TABLE bookings_settings
    ADD COLUMN IF NOT EXISTS sms_enabled TINYINT(1) NOT NULL DEFAULT 0 AFTER cancel_booking_limit_offset_hours,
    ADD COLUMN IF NOT EXISTS waitlist_enabled TINYINT(1) NOT NULL DEFAULT 0 AFTER sms_enabled,
    ADD COLUMN IF NOT EXISTS waitlist_max_size INT NOT NULL DEFAULT 0 AFTER waitlist_enabled,
    ADD COLUMN IF NOT EXISTS waitlist_slot_expiry_minutes INT NOT NULL DEFAULT 15 AFTER waitlist_max_size;

CREATE TABLE IF NOT EXISTS booking_waitlist (
    id             VARCHAR(64) NOT NULL PRIMARY KEY,
    merchant_id    VARCHAR(64) NOT NULL,
    customer_id    VARCHAR(64) NULL,
    party_size     INT NOT NULL DEFAULT 1,
    customer_name  VARCHAR(255) NOT NULL,
    customer_phone VARCHAR(50) NOT NULL,
    notes          TEXT NULL,
    status         ENUM('waiting', 'notified', 'seated', 'expired', 'cancelled') NOT NULL DEFAULT 'waiting',
    notified_at    DATETIME NULL,
    expires_at     DATETIME NULL,
    created_at     DATETIME NOT NULL DEFAULT UTC_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT UTC_TIMESTAMP ON UPDATE UTC_TIMESTAMP,
    INDEX idx_waitlist_merchant_status_created (merchant_id, status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS booking_events (
    id          VARCHAR(64) NOT NULL PRIMARY KEY,
    merchant_id VARCHAR(64) NOT NULL,
    booking_id  INT NULL,             -- bookings.booking_id (AUTO_INCREMENT) ; NULL pour un event waitlist pur
    waitlist_id VARCHAR(64) NULL,     -- booking_waitlist.id ; NULL pour un event réservation pur
    event_type  VARCHAR(64) NOT NULL, -- ex: no_show, waitlist_notified, sms_reconfirmed, sms_cancelled
    source      VARCHAR(64) NULL,     -- ex: pos, system, sms, public
    actor       VARCHAR(64) NULL,     -- SYSTEM | CUSTOMER | user_id staff
    metadata    JSON NULL,
    created_at  DATETIME NOT NULL DEFAULT UTC_TIMESTAMP,
    INDEX idx_booking_events_merchant_booking (merchant_id, booking_id),
    INDEX idx_booking_events_merchant_waitlist (merchant_id, waitlist_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
