-- RBAC lot 8: removes pos.access and pos.discount.apply from the permission
-- catalog. Neither ever guarded a route (RBAC lot 7 audit,
-- docs/RBAC_CLIENTS.md; RBAC lot 8 mapping, docs/RBAC_ROUTES.md) and no
-- replacement is planned — see docs/decisions.md, "RBAC lot 8 — dépréciation
-- de pos.access et pos.discount.apply" (2026-08-27), for the full rationale
-- and for why the system role "Employé polyvalent" (the only role that ever
-- held these two by default — see internal/modules/roles/repository.go,
-- systemRolePermissions[SystemKeyStaff]) ends up with zero permissions after
-- this migration, on purpose.
--
-- Order matters, same FK reasoning as migration 095's down: role_permissions
-- rows referencing these keys must go first, across every role of every
-- merchant — not just the system "staff" role, since a merchant may have
-- duplicated a custom role from it (see
-- roles.Repository.CreateRole/duplicate_from_role_id) and picked up a copy of
-- these grants before this lot.
DELETE FROM role_permissions WHERE permission_key IN ('pos.access', 'pos.discount.apply');

DELETE FROM permissions WHERE key IN ('pos.access', 'pos.discount.apply');
