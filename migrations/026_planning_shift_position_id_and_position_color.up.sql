ALTER TABLE planning_shifts
  ADD COLUMN position_id VARCHAR(64) NULL AFTER employee_id,
  ADD KEY idx_planning_shifts_position_id (position_id);

ALTER TABLE planning_positions
  ADD COLUMN color CHAR(7) NULL AFTER label;

UPDATE planning_positions
SET color = CASE MOD(CRC32(CONCAT(merchant_id, ':', LOWER(TRIM(label)))), 12)
  WHEN 0 THEN '#3b82f6'
  WHEN 1 THEN '#10b981'
  WHEN 2 THEN '#f59e0b'
  WHEN 3 THEN '#ef4444'
  WHEN 4 THEN '#8b5cf6'
  WHEN 5 THEN '#06b6d4'
  WHEN 6 THEN '#ec4899'
  WHEN 7 THEN '#84cc16'
  WHEN 8 THEN '#14b8a6'
  WHEN 9 THEN '#f97316'
  WHEN 10 THEN '#6366f1'
  ELSE '#94a3b8'
END
WHERE color IS NULL OR TRIM(color) = '';

ALTER TABLE planning_positions
  MODIFY COLUMN color CHAR(7) NOT NULL;

UPDATE planning_shifts s
JOIN (
  SELECT merchant_id, LOWER(TRIM(label)) AS normalized_label, MIN(id) AS resolved_position_id
  FROM planning_positions
  WHERE enabled = 1
  GROUP BY merchant_id, LOWER(TRIM(label))
) p
  ON p.merchant_id = s.merchant_id
 AND p.normalized_label = LOWER(TRIM(s.position))
SET s.position_id = p.resolved_position_id
WHERE s.enabled = 1
  AND s.position_id IS NULL
  AND s.position IS NOT NULL
  AND TRIM(s.position) <> '';

UPDATE planning_shifts s
JOIN planning_positions p
  ON p.id = s.position_id
 AND p.merchant_id = s.merchant_id
 AND p.enabled = 1
SET s.position = p.label
WHERE s.enabled = 1
  AND s.position_id IS NOT NULL
  AND (s.position IS NULL OR TRIM(s.position) <> p.label);

-- Note the returned number after running this migration to audit unresolved legacy labels.
SELECT COUNT(1) AS unresolved_planning_shifts_position_id
FROM planning_shifts s
WHERE s.enabled = 1
  AND s.position IS NOT NULL
  AND TRIM(s.position) <> ''
  AND s.position_id IS NULL;