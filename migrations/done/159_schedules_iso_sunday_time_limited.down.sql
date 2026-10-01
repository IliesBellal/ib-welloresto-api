-- Reverts 159_schedules_iso_sunday_time_limited.up.sql : non réversible à
-- l'identique. Les dimanches historiquement stockés en 7 (convention ISO,
-- données antérieures au back-office actuel) et ceux stockés en 0 ont été
-- fusionnés ; l'ancienne valeur de is_time_limited n'est pas conservée.
-- Aucun retour arrière de données n'est proposé : restaurer une sauvegarde
-- si nécessaire.
SELECT 1;
