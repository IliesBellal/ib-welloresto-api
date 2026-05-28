CREATE TABLE IF NOT EXISTS planning_positions (
  id VARCHAR(64) NOT NULL,
  merchant_id VARCHAR(64) NOT NULL,
  label VARCHAR(150) NOT NULL,
  sort_order INT NOT NULL DEFAULT 0,
  active TINYINT(1) NOT NULL DEFAULT 1,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  deleted_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_planning_positions_merchant (merchant_id),
  KEY idx_planning_positions_merchant_label (merchant_id, label),
  KEY idx_planning_positions_merchant_sort (merchant_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE employees
  ADD COLUMN position_id VARCHAR(64) NULL AFTER last_name,
  ADD COLUMN position_note TEXT NULL AFTER position_id;

INSERT INTO planning_positions (
  id,
  merchant_id,
  label,
  sort_order,
  active,
  enabled,
  created_at,
  updated_at
)
SELECT CONCAT('plan-pos-', UUID()), src.merchant_id, src.label, 0, 1, 1, UTC_TIMESTAMP(), UTC_TIMESTAMP()
FROM (
  SELECT DISTINCT merchant_id, TRIM(position) AS label
  FROM employees
  WHERE enabled = 1 AND TRIM(position) <> ''
) AS src;

INSERT INTO planning_positions (
  id,
  merchant_id,
  label,
  sort_order,
  active,
  enabled,
  created_at,
  updated_at
)
SELECT CONCAT('plan-pos-', UUID()), e.merchant_id, 'Sans poste', 9999, 1, 1, UTC_TIMESTAMP(), UTC_TIMESTAMP()
FROM employees e
LEFT JOIN planning_positions p
  ON p.merchant_id = e.merchant_id
  AND p.label = 'Sans poste'
  AND p.enabled = 1
WHERE e.enabled = 1
  AND (e.position IS NULL OR TRIM(e.position) = '')
  AND p.id IS NULL
GROUP BY e.merchant_id;

UPDATE employees e
JOIN planning_positions p
  ON p.merchant_id = e.merchant_id
  AND p.label = TRIM(e.position)
  AND p.enabled = 1
SET e.position_id = p.id
WHERE e.enabled = 1
  AND TRIM(e.position) <> '';

UPDATE employees e
JOIN planning_positions p
  ON p.merchant_id = e.merchant_id
  AND p.label = 'Sans poste'
  AND p.enabled = 1
SET e.position_id = p.id
WHERE e.enabled = 1
  AND (e.position IS NULL OR TRIM(e.position) = '');

ALTER TABLE employees
  MODIFY COLUMN position_id VARCHAR(64) NOT NULL,
  ADD KEY idx_employees_position_id (position_id);

ALTER TABLE employees
  DROP COLUMN position;