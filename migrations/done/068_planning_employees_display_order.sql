ALTER TABLE employees
ADD COLUMN display_order INT NOT NULL DEFAULT 0 AFTER position_id;

UPDATE employees e
SET e.display_order = (
  SELECT COUNT(1)
  FROM employees e2
  WHERE e2.merchant_id = e.merchant_id
    AND e2.enabled = 1
    AND (
      e2.last_name < e.last_name
      OR (e2.last_name = e.last_name AND e2.first_name < e.first_name)
      OR (e2.last_name = e.last_name AND e2.first_name = e.first_name AND e2.id <= e.id)
    )
)
WHERE e.enabled = 1;

ALTER TABLE employees
ADD KEY idx_employees_merchant_display_order (merchant_id, display_order);
