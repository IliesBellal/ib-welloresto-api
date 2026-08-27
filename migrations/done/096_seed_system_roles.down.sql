-- Reverts the seeding performed by `go run ./cmd/seed_system_roles`
-- (096_seed_system_roles.up.sql). This direction IS plain SQL: undoing the
-- seed needs no UUID generation, only deleting what was created and clearing
-- the pointer to it.
--
-- Clears default_role_id only where it still points at a system role, so a
-- merchant that was manually repointed to a custom role in the meantime keeps
-- its choice.
UPDATE merchant
SET default_role_id = NULL
WHERE default_role_id IN (SELECT id FROM roles WHERE system_key IS NOT NULL);

-- Cascades into role_permissions via role_permissions.role_id ON DELETE CASCADE.
DELETE FROM roles WHERE system_key IS NOT NULL;
