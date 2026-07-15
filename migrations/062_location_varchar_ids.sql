-- =============================================================================
-- Migration : VARCHAR(64) IDs — tables du plan de salle
-- Numéro    : 06X — à placer après les migrations Lot 1 réservation (053–059)
-- Auteur    : généré Phase 0 — Refonte plan de salle
-- Stratégie : conversion additives des INT existants en VARCHAR
--             Valeurs existantes : conservées telles quelles sous forme de string
--             (ex. location_id = 1  →  '1')
--             Nouveaux enregistrements : GeneratePrefixedID("loc"|"flr"|"fra")
--             côté Go — ex. 'loc-550e8400-e29b-41d4-a716-446655440000'
--
-- Tables modifiées :
--   floors          id INT(PK)          → VARCHAR(64)
--   locations       location_id INT(PK) → VARCHAR(64)
--                   floor_id INT        → VARCHAR(64)
--   floor_areas     id INT(PK)          → VARCHAR(64)
--                   floor_id INT        → VARCHAR(64)
--   order_location  location_id INT     → VARCHAR(64)
--   booked_location location_id INT     → VARCHAR(64)
--                   id INT AUTO_INCREMENT → supprimé (remplacé par PK composite)
--   qrcodes         location_id INT     → VARCHAR(64)
--   kiosks          location_id INT     → VARCHAR(64)
--
-- LIVE-SAFE :
--   • Aucune donnée supprimée. Aucune colonne métier retirée.
--   • Chaque ALTER TABLE est indépendant — en cas d'erreur partielle, les
--     étapes suivantes peuvent être rejouées sans risque de corruption.
--   • À exécuter hors pic de trafic (ALTER TABLE pose un metadata lock bref
--     sur chaque table modifiée). Durée estimée : < 5 s sur volumes POS typ.
-- =============================================================================

-- Désactivation temporaire des FK checks (précaution — pas de FK déclarées
-- en base héritage PHP, mais par sécurité si des FK ont été ajoutées depuis)
SET FOREIGN_KEY_CHECKS = 0;

-- =============================================================================
-- BLOC 1 — floors
-- PK : id INT AUTO_INCREMENT → VARCHAR(64)
-- Référencé par : locations.floor_id, floor_areas.floor_id
-- =============================================================================

ALTER TABLE floors
  DROP PRIMARY KEY,
  MODIFY COLUMN id VARCHAR(64) NOT NULL;

ALTER TABLE floors
  ADD PRIMARY KEY (id);

-- =============================================================================
-- BLOC 2 — locations (FK floor_id d'abord, puis PK location_id)
-- =============================================================================

-- 2a. floor_id : aligner le type avec floors.id désormais VARCHAR
ALTER TABLE locations
  MODIFY COLUMN floor_id VARCHAR(64) DEFAULT NULL;

-- 2b. PK location_id INT → VARCHAR(64)
--     Référencé par : order_location.location_id, booked_location.location_id,
--                    qrcodes.location_id, kiosks.location_id
ALTER TABLE locations
  DROP PRIMARY KEY,
  MODIFY COLUMN location_id VARCHAR(64) NOT NULL;

ALTER TABLE locations
  ADD PRIMARY KEY (location_id);

-- =============================================================================
-- BLOC 3 — floor_areas
-- PK : id INT → VARCHAR(64)
-- FK : floor_id INT → VARCHAR(64)
-- Aucun référent externe connu (pas d'autre table qui référence floor_areas.id)
-- =============================================================================

ALTER TABLE floor_areas
  DROP PRIMARY KEY,
  MODIFY COLUMN id      VARCHAR(64) NOT NULL,
  MODIFY COLUMN floor_id VARCHAR(64) NOT NULL;

ALTER TABLE floor_areas
  ADD PRIMARY KEY (id);

-- =============================================================================
-- BLOC 4 — order_location
-- PK composite (order_id INT, location_id INT) → (order_id INT, location_id VARCHAR)
-- Note : order_id reste INT (orders.order_id non migré dans ce lot)
-- =============================================================================

ALTER TABLE order_location
  DROP PRIMARY KEY,
  MODIFY COLUMN location_id VARCHAR(64) NOT NULL;

ALTER TABLE order_location
  ADD PRIMARY KEY (order_id, location_id);

-- Index sur location_id seul (lectures plan de salle — occupation par table)
-- Vérifie s'il existe déjà avant de créer
-- (MySQL 8+ : CREATE INDEX IF NOT EXISTS — pas supporté universellement,
--  on drop+recreate pour compatibilité MySQL 5.7)
ALTER TABLE order_location
  ADD INDEX idx_order_location_location_id (location_id);

-- =============================================================================
-- BLOC 5 — booked_location
-- Avant  : id INT AUTO_INCREMENT PK, UNIQUE uq_booked_location(booking_id, location_id)
-- Après  : PK composite (booking_id, location_id) + index location_id seul
-- Rationale : id auto-increment jamais lu ni référencé côté Go (rapport phase0-api §1.1)
--             La contrainte UNIQUE (052) devient la PK — sémantique plus claire,
--             une ligne par (réservation, table), pas de colonne fantôme.
-- =============================================================================

-- 5a. Supprimer l'ancienne PK (id)
ALTER TABLE booked_location
  DROP PRIMARY KEY;

-- 5b. Supprimer la contrainte UNIQUE posée par la migration 052
--     (elle devient redondante une fois transformée en PK)
ALTER TABLE booked_location
  DROP INDEX uq_booked_location;

-- 5c. Supprimer la colonne id devenue inutile
ALTER TABLE booked_location
  DROP COLUMN id;

-- 5d. Migrer location_id vers VARCHAR(64)
ALTER TABLE booked_location
  MODIFY COLUMN location_id VARCHAR(64) NOT NULL;

-- 5e. PK composite
ALTER TABLE booked_location
  ADD PRIMARY KEY (booking_id, location_id);

-- 5f. Index sur location_id seul
--     (utilisé par : contrôle de conflit table×créneau, vue plan de salle,
--      résolution résa en cours côté ScannOrder)
ALTER TABLE booked_location
  ADD INDEX idx_booked_location_location_id (location_id);

-- =============================================================================
-- BLOC 6 — qrcodes.location_id
-- Nullable — QR de table pointant vers locations.location_id
-- =============================================================================

ALTER TABLE qrcodes
  MODIFY COLUMN location_id VARCHAR(64) DEFAULT NULL;

-- =============================================================================
-- BLOC 7 — kiosks.location_id
-- Nullable — métadonnée d'appareil, sans logique métier active (rapport phase0 §Axe5)
-- =============================================================================

ALTER TABLE kiosks
  MODIFY COLUMN location_id VARCHAR(64) DEFAULT NULL;

-- =============================================================================
-- Réactivation des FK checks
-- =============================================================================

SET FOREIGN_KEY_CHECKS = 1;

-- =============================================================================
-- VÉRIFICATION POST-MIGRATION (à exécuter manuellement après déploiement)
-- =============================================================================
-- Vérifier les types :
--   SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY
--   FROM information_schema.COLUMNS
--   WHERE TABLE_SCHEMA = DATABASE()
--     AND TABLE_NAME IN ('floors','locations','floor_areas',
--                        'order_location','booked_location','qrcodes','kiosks')
--     AND COLUMN_NAME IN ('id','location_id','floor_id')
--   ORDER BY TABLE_NAME, ORDINAL_POSITION;
--
-- Vérifier les index :
--   SHOW INDEX FROM booked_location;
--   SHOW INDEX FROM order_location;
--
-- Vérifier une table active :
--   SELECT location_id, floor_id, location_name, seats FROM locations LIMIT 10;
--   SELECT id, name FROM floors LIMIT 10;
--
-- Vérifier les jointures critiques (plan de salle commandes — doit retourner les mêmes lignes qu'avant) :
--   SELECT l.location_id, l.location_name, ol.order_id
--   FROM locations l
--   LEFT JOIN order_location ol ON ol.location_id = l.location_id
--   WHERE l.merchant_id = <un_merchant_id_de_test>
--   LIMIT 20;