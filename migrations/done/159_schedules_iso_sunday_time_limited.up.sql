-- Cohérence entre ce que le back-office affiche et ce que le pricing /
-- Kiosk / ScanNOrder appliquent (voir docs/KIOSK_DECISIONS.md, 2026-09-27).
--
-- 1. Dimanche : le back-office envoyait day_of_week = 0 (convention JS
--    0 = dimanche) alors que toutes les évaluations utilisent la convention
--    ISO 1 = lundi … 7 = dimanche. Les créneaux du dimanche stockés en 0
--    n'étaient jamais actifs (promotions), ou refusés (disponibilités). L'API
--    ramène désormais 0 à 7 à l'écriture ; cette migration corrige l'existant.
--
-- 2. Restriction horaire des promotions : le back-office ne renseignait
--    jamais is_time_limited (il ne gère que la liste des créneaux). Une
--    promotion affichée avec des créneaux s'appliquait donc à toute heure.
--    L'API déduit désormais is_time_limited de la présence de créneaux ; cette
--    migration aligne l'existant. EFFET VISIBLE : une promotion qui a des
--    créneaux actifs ne s'applique plus qu'à ces créneaux (ce que le
--    back-office affichait déjà).
--
-- Idempotente : peut être rejouée sans effet.
UPDATE discounts_schedules SET day_of_week = 7 WHERE day_of_week = 0;
UPDATE availabilities_schedules SET day_of_week = 7 WHERE day_of_week = 0;

UPDATE discounts d
SET is_time_limited = EXISTS (
    SELECT 1 FROM discounts_schedules s
    WHERE s.discount_id = d.discount_id AND s.enabled = TRUE
)
WHERE d.is_time_limited IS DISTINCT FROM EXISTS (
    SELECT 1 FROM discounts_schedules s
    WHERE s.discount_id = d.discount_id AND s.enabled = TRUE
);
