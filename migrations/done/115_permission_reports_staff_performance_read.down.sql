-- Reverts 115_permission_reports_staff_performance_read.up.sql.
--
-- role_permissions rows referencing the key are purged first — same FK
-- reasoning as migration 100's/103's down.

DELETE FROM role_permissions WHERE permission_key = 'reports.staff_performance.read';

DELETE FROM permissions WHERE key = 'reports.staff_performance.read';
