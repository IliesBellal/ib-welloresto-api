ALTER TABLE packages
    ADD COLUMN planning_enabled TINYINT(1) NOT NULL DEFAULT 0,
    ADD COLUMN haccp_enabled TINYINT(1) NOT NULL DEFAULT 1,
    ADD COLUMN stock_enabled TINYINT(1) NOT NULL DEFAULT 0,
    ADD COLUMN scannorder_enabled TINYINT(1) NOT NULL DEFAULT 0,
    ADD COLUMN bookings_enabled TINYINT(1) NOT NULL DEFAULT 1;

ALTER TABLE subscriptions
    ADD COLUMN planning_enabled TINYINT(1) NOT NULL DEFAULT 0,
    ADD COLUMN haccp_enabled TINYINT(1) NOT NULL DEFAULT 1,
    ADD COLUMN stock_enabled TINYINT(1) NOT NULL DEFAULT 0,
    ADD COLUMN scannorder_enabled TINYINT(1) NOT NULL DEFAULT 0,
    ADD COLUMN bookings_enabled TINYINT(1) NOT NULL DEFAULT 1;

-- Bootstrap package defaults from the current behavior so the migration does not disable features unexpectedly.
UPDATE packages
SET planning_enabled = hr_management,
    haccp_enabled = TRUE,
    stock_enabled = CASE WHEN stock_management > 0 THEN TRUE ELSE FALSE END,
    scannorder_enabled = scannorder_ready,
    bookings_enabled = TRUE;

-- Copy package defaults to the merchant subscriptions. From this point on, subscription values are the effective rights.
UPDATE subscriptions s
LEFT JOIN packages p ON p.id = s.package_id
SET s.planning_enabled = COALESCE(p.planning_enabled, FALSE),
    s.haccp_enabled = COALESCE(p.haccp_enabled, TRUE),
    s.stock_enabled = COALESCE(p.stock_enabled, FALSE),
    s.scannorder_enabled = COALESCE(p.scannorder_enabled, FALSE),
    s.bookings_enabled = COALESCE(p.bookings_enabled, TRUE);