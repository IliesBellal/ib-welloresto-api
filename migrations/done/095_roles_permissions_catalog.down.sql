-- Reverts 095_roles_permissions_catalog.up.sql.
--
-- Run this only after 096's down has removed every role_permissions row
-- referencing these keys (roles.down cascades into role_permissions) —
-- otherwise the DELETE below fails on the role_permissions.permission_key FK,
-- which is exactly the safety net it is supposed to be.

DELETE FROM permissions WHERE key IN (
    'pos.access',
    'pos.ticket.reopen',
    'pos.discount.apply',
    'pos.refund',
    'pos.cash_drawer.open',
    'catalog.manage',
    'inventory.manage',
    'haccp.manage',
    'customers.manage',
    'staff.manage',
    'staff.schedule.manage',
    'reports.sales.read',
    'reports.financial.read',
    'settings.manage'
);
