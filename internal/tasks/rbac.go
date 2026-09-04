package tasks

import (
	"context"

	"welloresto-api/internal/modules/roles"

	"go.uber.org/zap"
)

// ReconcileSystemRolePermissions keeps every merchant's "admin" system role in
// sync with the permission catalog, so a catalog migration never again
// depends on someone remembering to run `go run ./cmd/seed_system_roles` by
// hand. That exact gap (RBAC lot 10, migration 103) left 29 of 30 staging
// admin roles missing 5 permissions for over a week — see docs/decisions.md.
//
// Delegates to roles.Repository.ReconcileSystemRoles, the same code path as
// the CLI (cmd/seed_system_roles). Idempotent and additive only: never
// revokes a permission, never touches an already-set default_role_id, never
// touches the client-editable "staff" role past its creation. One merchant's
// failure is logged and does not stop the others.
func (tm *TasksManager) ReconcileSystemRolePermissions() {
	repo := roles.NewRepository(tm.DB)
	results, err := repo.ReconcileSystemRoles(context.Background())
	if err != nil {
		tm.logError("system role reconciliation failed", zap.Error(err))
		return
	}

	failed := 0
	for _, res := range results {
		if res.Err == nil {
			continue
		}
		failed++
		tm.logError("system role reconciliation failed for merchant",
			zap.String("merchant_id", res.MerchantID), zap.Error(res.Err))
	}

	tm.logInfo("system role reconciliation finished",
		zap.Int("merchants", len(results)), zap.Int("failed", failed))
}
