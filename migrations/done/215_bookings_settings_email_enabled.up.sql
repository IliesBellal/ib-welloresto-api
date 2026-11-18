-- Canal email des réservations désactivable par l'établissement
-- (back-office : Paramètres réservations > Canaux de communication).
-- Vrai par défaut : le comportement actuel (emails toujours envoyés) est conservé.
ALTER TABLE bookings_settings
    ADD COLUMN IF NOT EXISTS email_enabled BOOLEAN NOT NULL DEFAULT TRUE;
