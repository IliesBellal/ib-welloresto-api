-- ============================================================================
-- 052 down — retire la contrainte UNIQUE. Le dédoublonnage du up est
-- irréversible (les doublons supprimés étaient des données erronées, sans
-- information propre : la table ne porte que le couple booking_id/location_id).
-- ============================================================================

ALTER TABLE booked_location
    DROP INDEX uq_booked_location;
