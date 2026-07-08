-- ============================================================================
-- 052 — Dédoublonnage de booked_location puis contrainte UNIQUE
-- (booking_id, location_id) : empêche les doublons d'affectation d'une même
-- table à une même réservation (cadrage §1.1, E6). Le conflit table × créneau
-- (deux résas différentes, même table, créneaux chevauchants) ne peut pas être
-- une contrainte SQL (dépend des dates) : contrôle applicatif en transaction
-- (T-07).
--
-- booked_location n'a pas de clé primaire : le dédoublonnage passe par une
-- table de recopie DISTINCT + bascule par RENAME (atomique). Volumétrie faible
-- (affectations de tables) ; à exécuter hors pic d'écriture.
-- Prérequis : migration 050 appliquée (index idx_booked_location_location).
-- ============================================================================

CREATE TABLE booked_location_dedup LIKE booked_location;

INSERT INTO booked_location_dedup (booking_id, location_id)
SELECT DISTINCT booking_id, location_id
FROM booked_location;

RENAME TABLE booked_location TO booked_location_old,
             booked_location_dedup TO booked_location;

DROP TABLE booked_location_old;

ALTER TABLE booked_location
    ADD CONSTRAINT uq_booked_location UNIQUE (booking_id, location_id);
