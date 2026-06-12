UPDATE planning_shifts s
JOIN planning_positions p
  ON p.id = s.position_id
 AND p.merchant_id = s.merchant_id
SET s.position = COALESCE(NULLIF(TRIM(s.position), ''), p.label)
WHERE s.position_id IS NOT NULL;

ALTER TABLE planning_positions
  DROP COLUMN color;

ALTER TABLE planning_shifts
  DROP INDEX idx_planning_shifts_position_id,
  DROP COLUMN position_id;