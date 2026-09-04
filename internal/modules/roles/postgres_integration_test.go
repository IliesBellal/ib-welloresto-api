//go:build postgres_integration

package roles

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	redisclient "welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	usersModule "welloresto-api/internal/modules/users"
	"welloresto-api/internal/permission"
)

func TestRoles_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
		VALUES ('ITest Roles Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-roles-itest', 'https://example.com', '0600000000', 'mtok-roles-itest', 'Europe/Paris', 1.0, 2.0)
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE merchant_id = $1 AND user_id = 'itest-roles-user'`, merchantID)
		_, _ = db.ExecContext(ctx, `UPDATE merchant SET default_role_id = NULL WHERE id = $1`, merchantIntID)
		_, _ = db.ExecContext(ctx, `DELETE FROM roles WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
	})

	// --- 1. Les 13 droits du catalogue sont présents après migration 100. ---
	catalog, err := repo.ListPermissions(ctx)
	if err != nil {
		t.Fatalf("ListPermissions: %v", err)
	}
	present := make(map[permission.Key]bool, len(catalog))
	for _, p := range catalog {
		present[p.Key] = true
	}
	for _, key := range permission.All {
		if !present[key] {
			t.Errorf("permission %q from internal/permission is missing from the permissions table — were migrations 095/097 applied?", key)
		}
	}

	// --- 2. EnsureSystemRoles crée exactement deux rôles système. ---
	adminID, staffID, err := repo.EnsureSystemRoles(ctx, merchantID)
	if err != nil {
		t.Fatalf("EnsureSystemRoles: %v", err)
	}
	if adminID == "" || staffID == "" {
		t.Fatalf("expected non-empty role ids, got admin=%q staff=%q", adminID, staffID)
	}

	roleList, err := repo.ListRoles(ctx, merchantID)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	systemRoles := 0
	for _, role := range roleList {
		if role.SystemKey != nil {
			systemRoles++
		}
	}
	if systemRoles != 2 {
		t.Fatalf("expected exactly 2 system roles for the merchant, got %d (roles=%+v)", systemRoles, roleList)
	}

	// --- 3. Le rôle admin porte les 13 droits, le rôle staff n'en porte
	// aucun (RBAC lot 8 : pos.access et pos.discount.apply, les deux seuls
	// droits que "staff" portait par défaut, sont sortis du catalogue — voir
	// systemRolePermissions[SystemKeyStaff] et docs/decisions.md). ---
	adminPerms, err := repo.GetRolePermissions(ctx, adminID)
	if err != nil {
		t.Fatalf("GetRolePermissions(admin): %v", err)
	}
	if len(adminPerms) != len(permission.All) {
		t.Fatalf("expected admin role to carry all %d permissions, got %d: %+v", len(permission.All), len(adminPerms), adminPerms)
	}

	staffPerms, err := repo.GetRolePermissions(ctx, staffID)
	if err != nil {
		t.Fatalf("GetRolePermissions(staff): %v", err)
	}
	if len(staffPerms) != 0 {
		t.Fatalf("expected staff role to carry zero permissions by default (RBAC lot 8), got %d: %+v", len(staffPerms), staffPerms)
	}

	// --- 4. EnsureSystemRoles est idempotente. ---
	adminID2, staffID2, err := repo.EnsureSystemRoles(ctx, merchantID)
	if err != nil {
		t.Fatalf("EnsureSystemRoles (2nd call): %v", err)
	}
	if adminID2 != adminID || staffID2 != staffID {
		t.Fatalf("EnsureSystemRoles is not idempotent: 1st call (%s, %s), 2nd call (%s, %s)", adminID, staffID, adminID2, staffID2)
	}
	roleListAfter, err := repo.ListRoles(ctx, merchantID)
	if err != nil {
		t.Fatalf("ListRoles (after 2nd EnsureSystemRoles): %v", err)
	}
	if len(roleListAfter) != len(roleList) {
		t.Fatalf("expected role count to stay at %d after a 2nd EnsureSystemRoles call, got %d", len(roleList), len(roleListAfter))
	}

	// --- 5. Supprimer un rôle porté par une ligne users_rights échoue en base. ---
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users_rights (user_id, merchant_id, token, role_id, enabled, login_enabled)
		VALUES ('itest-roles-user', $1, 'itest-roles-token', $2, true, true)
	`, merchantID, staffID); err != nil {
		t.Fatalf("seed users_rights wearing the staff role: %v", err)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM roles WHERE id = $1`, staffID); err == nil {
		t.Fatalf("expected DELETE FROM roles to fail while a users_rights row still references it via role_id, but it succeeded")
	}
}

// TestEnsureSystemRoles_StaffCustomizationSurvivesReRun is the lot 2.5
// regression test: a merchant's customized "staff" role (renamed AND with
// hand-edited permissions) must survive any number of later EnsureSystemRoles
// calls untouched, while "admin" — not client-editable — still gets a missing
// permission backfilled the same run. Before the fix, ensureSystemRole
// reconciled both roles' permissions on every call, so a permission the
// merchant had deliberately removed from staff would silently reappear.
func TestEnsureSystemRoles_StaffCustomizationSurvivesReRun(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
		VALUES ('ITest Roles Staff Custom', 'addr', '1', 'street', '75001', 'Paris', 'siret-roles-itest-staffcustom', 'https://example.com', '0600000001', 'mtok-roles-sc', 'Europe/Paris', 1.0, 2.0)
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `UPDATE merchant SET default_role_id = NULL WHERE id = $1`, merchantIntID)
		_, _ = db.ExecContext(ctx, `DELETE FROM roles WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
	})

	// --- 1. Établissement créé, rôles système initiaux. ---
	adminID, staffID, err := repo.EnsureSystemRoles(ctx, merchantID)
	if err != nil {
		t.Fatalf("EnsureSystemRoles (initial): %v", err)
	}

	// --- 2. Le client personnalise "staff" : nom ET permissions. Baseline
	// vide depuis RBAC lot 8 (systemRolePermissions[SystemKeyStaff]) — la
	// personnalisation ici est donc une grant pure, plus une revoke. ---
	const customStaffName = "Serveur du soir"
	if _, err := db.ExecContext(ctx, `UPDATE roles SET name = $1 WHERE id = $2`, customStaffName, staffID); err != nil {
		t.Fatalf("rename staff role: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO role_permissions (role_id, permission_key) VALUES ($1, $2)`, staffID, string(permission.CatalogManage)); err != nil {
		t.Fatalf("grant catalog.manage to staff: %v", err)
	}

	// --- 3. Un droit manque sur "admin" (ex. catalogue pas encore rattrapé). ---
	if _, err := db.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = $1 AND permission_key = $2`, adminID, string(permission.POSStatusManage)); err != nil {
		t.Fatalf("simulate missing permission on admin: %v", err)
	}

	// --- 4. EnsureSystemRoles relancée deux fois (rattrapage de migration, restauration...). ---
	for i := 0; i < 2; i++ {
		if _, _, err := repo.EnsureSystemRoles(ctx, merchantID); err != nil {
			t.Fatalf("EnsureSystemRoles (rerun %d): %v", i+1, err)
		}
	}

	// --- 5. Les deux personnalisations de "staff" ont survécu. ---
	staffRole, err := repo.GetRoleByID(ctx, merchantID, staffID)
	if err != nil {
		t.Fatalf("GetRoleByID(staff): %v", err)
	}
	if staffRole == nil {
		t.Fatalf("staff role disappeared")
	}
	if staffRole.Name != customStaffName {
		t.Fatalf("expected staff role name to survive as %q, got %q", customStaffName, staffRole.Name)
	}

	staffPerms, err := repo.GetRolePermissions(ctx, staffID)
	if err != nil {
		t.Fatalf("GetRolePermissions(staff): %v", err)
	}
	staffKeys := map[permission.Key]bool{}
	for _, p := range staffPerms {
		staffKeys[p.Key] = true
	}
	if !staffKeys[permission.CatalogManage] {
		t.Fatalf("expected catalog.manage to stay granted on staff, but it is missing: %+v", staffPerms)
	}
	if len(staffPerms) != 1 {
		t.Fatalf("expected staff to carry exactly {catalog.manage} (1 permission: the empty RBAC lot 8 baseline plus the client's own grant), got %d: %+v", len(staffPerms), staffPerms)
	}

	// --- 6. Le droit manquant sur "admin" a bien été rattrapé. ---
	adminPerms, err := repo.GetRolePermissions(ctx, adminID)
	if err != nil {
		t.Fatalf("GetRolePermissions(admin): %v", err)
	}
	if len(adminPerms) != len(permission.All) {
		t.Fatalf("expected admin role to carry all %d permissions again after the backfill, got %d: %+v", len(permission.All), len(adminPerms), adminPerms)
	}
	adminKeys := map[permission.Key]bool{}
	for _, p := range adminPerms {
		adminKeys[p.Key] = true
	}
	if !adminKeys[permission.POSStatusManage] {
		t.Fatalf("expected pos.status.manage to be backfilled onto admin, but it is still missing: %+v", adminPerms)
	}
}

// TestNoCrossTenantRoleAssignment is a permanent RBAC lot 4 safety net (see
// docs/RBAC_BASCULE.md §3, and the ad-hoc verification run as part of the
// admin-role bascule itself). A users_rights row whose role_id points at a
// role belonging to a DIFFERENT merchant would grant someone rights on an
// establishment that is not theirs — the single most dangerous outcome a bug
// in cmd/assign_admin_role, migration 099, or any future role-assignment code
// path could produce. This runs against whatever database this test suite is
// pointed at (dev, CI, or a real environment snapshot) and must always find
// zero such rows.
func TestNoCrossTenantRoleAssignment(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM users_rights ur
		JOIN roles r ON r.id = ur.role_id
		WHERE r.merchant_id <> ur.merchant_id
	`).Scan(&count); err != nil {
		t.Fatalf("cross-tenant role assignment check: %v", err)
	}
	if count != 0 {
		t.Fatalf("found %d users_rights row(s) whose role_id belongs to a DIFFERENT merchant than the row itself — this is a cross-tenant privilege leak, see docs/RBAC_BASCULE.md §3", count)
	}
}

// TestSystemAdminRolesContainFullCatalog_Postgres is the RBAC lot 11
// invariant that UserLoginRow.Has()'s system_key short-circuit
// (internal/modules/auth/permissions.go) depends on for its own eventual
// removal: every system_key='admin' role must carry every non-deprecated
// catalog permission. Like TestNoCrossTenantRoleAssignment, this runs against
// whatever database this suite is pointed at (dev, CI, or — by overriding
// POSTGRES_URL for a one-off manual run — a real environment snapshot) and
// must always find zero incomplete admin roles.
//
// This exact invariant was violated in staging for over a week after RBAC
// lot 10 (migration 103) added 5 catalog keys without a repeat
// `go run ./cmd/seed_system_roles` — 29 of 30 admin roles were missing
// pos.analytics/bookings.manage/platforms.manage/kiosk.manage/
// seating_plan.manage (see docs/decisions.md). This test is what should have
// caught that, and ReconcileSystemRolePermissions (internal/tasks/rbac.go) is
// what now prevents it recurring without manual intervention.
func TestSystemAdminRolesContainFullCatalog_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	incomplete, err := repo.FindIncompleteAdminRoles(ctx)
	if err != nil {
		t.Fatalf("FindIncompleteAdminRoles: %v", err)
	}
	if len(incomplete) != 0 {
		t.Fatalf("found %d incomplete admin role(s) — a catalog permission was added without EnsureSystemRoles ever reconciling these merchants' admin role against it: %+v", len(incomplete), incomplete)
	}
}

// TestReconcileSystemRoles_BackfillsIncompleteAdminRole_Postgres reproduces
// the exact staging gap described above at the repository level: an admin
// role missing a catalog permission it should have, then asserts
// ReconcileSystemRoles (the shared implementation behind cmd/seed_system_roles
// and the cron task) backfills it without being told which merchant or which
// key is missing.
func TestReconcileSystemRoles_BackfillsIncompleteAdminRole_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)

	_, merchantID := seedRolesTestMerchant(t, db, "reconcile-1")

	adminID, _, err := repo.EnsureSystemRoles(ctx, merchantID)
	if err != nil {
		t.Fatalf("EnsureSystemRoles: %v", err)
	}

	missingKey := permission.All[0]
	if _, err := db.ExecContext(ctx, `
		DELETE FROM role_permissions WHERE role_id = $1 AND permission_key = $2
	`, adminID, string(missingKey)); err != nil {
		t.Fatalf("simulate missing grant: %v", err)
	}

	incomplete, err := repo.FindIncompleteAdminRoles(ctx)
	if err != nil {
		t.Fatalf("FindIncompleteAdminRoles (before reconcile): %v", err)
	}
	found := false
	for _, r := range incomplete {
		if r.RoleID == adminID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected role %s to be reported incomplete before reconciling, got: %+v", adminID, incomplete)
	}

	results, err := repo.ReconcileSystemRoles(ctx)
	if err != nil {
		t.Fatalf("ReconcileSystemRoles: %v", err)
	}
	for _, res := range results {
		if res.MerchantID == merchantID && res.Err != nil {
			t.Fatalf("ReconcileSystemRoles failed for merchant %s: %v", merchantID, res.Err)
		}
	}

	perms, err := repo.GetRolePermissions(ctx, adminID)
	if err != nil {
		t.Fatalf("GetRolePermissions: %v", err)
	}
	if len(perms) != len(permission.All) {
		t.Fatalf("expected admin role to carry all %d permissions after reconciling, got %d: %+v", len(permission.All), len(perms), perms)
	}
}

// ---------------------------------------------------------------------------
// RBAC lot 6 — role administration API, exercised through the Service layer
// against real Postgres (dialect-correctness of the optimistic-locking CAS,
// the version bump, the staff.manage COUNT(DISTINCT ...) join — none of
// which sqlmock's unit tests in service_test.go can verify).
// ---------------------------------------------------------------------------

// seedRolesTestMerchant creates a bare merchant row (no satellites needed —
// this suite only touches merchant/roles/permissions/users/users_rights) and
// registers its cleanup.
func seedRolesTestMerchant(t *testing.T, db *sql.DB, siret string) (intID int64, strID string) {
	t.Helper()
	ctx := context.Background()
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
		VALUES ($1, 'addr', '1', 'street', '75001', 'Paris', $2, 'https://example.com', '0600000000', $3, 'Europe/Paris', 1.0, 2.0)
		RETURNING id`, "ITest Roles Lot6 "+siret, siret, "mtok-"+siret).Scan(&intID); err != nil {
		t.Fatalf("seed merchant %s: %v", siret, err)
	}
	strID = strconv.FormatInt(intID, 10)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE merchant_id = $1`, strID)
		_, _ = db.ExecContext(ctx, `UPDATE merchant SET default_role_id = NULL WHERE id = $1`, intID)
		_, _ = db.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id IN (SELECT id FROM roles WHERE merchant_id = $1)`, strID)
		_, _ = db.ExecContext(ctx, `DELETE FROM roles WHERE merchant_id = $1`, strID)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, intID)
	})
	return intID, strID
}

// seedRolesTestUser creates a users row and an enabled users_rights link for
// merchantID, wearing roleID (may be "" for a NULL role_id — the orphan case).
func seedRolesTestUser(t *testing.T, db *sql.DB, userID, merchantID, roleID, token string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, name, first_name, last_name, email, tel, password, token)
		VALUES ($1, 'ITest User', 'ITest', 'User', $1 || '@example.com', '+33600000000', 'x', $1 || '-utok')
		ON CONFLICT DO NOTHING`, userID); err != nil {
		t.Fatalf("seed user %s: %v", userID, err)
	}
	var roleArg interface{}
	if roleID != "" {
		roleArg = roleID
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users_rights (user_id, merchant_id, token, role_id, enabled, login_enabled)
		VALUES ($1, $2, $3, $4, TRUE, TRUE)`, userID, merchantID, token, roleArg); err != nil {
		t.Fatalf("seed users_rights for %s: %v", userID, err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, userID) })
}

func newRolesTestService(db *sql.DB, redis *redisclient.Client) *Service {
	return NewService(NewRepository(db), usersModule.NewUserRepository(db), nil, redis)
}

// TestRolesService_Postgres_FullLifecycle exercises create -> duplicate ->
// rename (version bump) -> replace permissions (version bump) -> members ->
// archive against real Postgres, proving the optimistic-locking UPDATE and
// the version/CAS SQL are dialect-correct (sqlmock only proves the Go call
// sequence, not that Postgres actually accepts and applies this SQL).
func TestRolesService_Postgres_FullLifecycle(t *testing.T) {
	db := pgtest.Open(t)
	_, merchantID := seedRolesTestMerchant(t, db, "lot6-lifecycle")
	svc := newRolesTestService(db, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "itest-admin", MerchantID: merchantID})

	created, err := svc.CreateRole(ctx, CreateRoleRequest{Name: "Chef de cuisine", Description: "Gère la cuisine"})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if created.Version != 1 || len(created.Permissions) != 0 {
		t.Fatalf("unexpected created role: %+v", created)
	}

	duplicated, err := svc.CreateRole(ctx, CreateRoleRequest{Name: "Chef adjoint", DuplicateFromRoleID: &created.ID})
	if err != nil {
		t.Fatalf("CreateRole (duplicate of empty-permission role): %v", err)
	}
	if len(duplicated.Permissions) != 0 {
		t.Fatalf("expected duplicate of a permission-less role to itself have none, got %+v", duplicated.Permissions)
	}

	renamed, err := svc.UpdateRole(ctx, created.ID, UpdateRoleRequest{Name: strPtr("Chef de cuisine (renommé)"), Version: created.Version})
	if err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	if renamed.Name != "Chef de cuisine (renommé)" || renamed.Version != 2 {
		t.Fatalf("unexpected renamed role: %+v", renamed.Role)
	}

	replaced, err := svc.ReplacePermissions(ctx, created.ID, ReplacePermissionsRequest{
		PermissionKeys: []permission.Key{permission.CatalogManage, permission.InventoryManage},
		Version:        renamed.Version,
	})
	if err != nil {
		t.Fatalf("ReplacePermissions: %v", err)
	}
	if replaced.Version != 3 || len(replaced.Permissions) != 2 {
		t.Fatalf("unexpected replaced role: %+v", replaced)
	}

	seedRolesTestUser(t, db, "itest-lot6-user1", merchantID, created.ID, "itest-lot6-tok1")
	members, err := svc.ListRoleMembers(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListRoleMembers: %v", err)
	}
	if len(members) != 1 || members[0].UserID != "itest-lot6-user1" {
		t.Fatalf("unexpected members: %+v", members)
	}

	// G5: cannot archive while a holder exists.
	if _, err := svc.ArchiveRole(ctx, created.ID); err == nil {
		t.Fatal("expected ArchiveRole to fail while a member holds the role")
	} else {
		var membersErr *RoleHasMembersError
		if !errors.As(err, &membersErr) || membersErr.Count != 1 {
			t.Fatalf("expected *RoleHasMembersError{Count:1}, got %v", err)
		}
	}

	// Move the holder off, then archive succeeds.
	if _, err := svc.SetUserRole(ctx, "itest-lot6-user1", SetUserRoleRequest{RoleID: duplicated.ID}); err != nil {
		t.Fatalf("SetUserRole (move off before archive): %v", err)
	}
	archived, err := svc.ArchiveRole(ctx, created.ID)
	if err != nil {
		t.Fatalf("ArchiveRole: %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Fatal("expected ArchivedAt to be set")
	}
}

// TestRolesService_Postgres_VersionConflict proves the optimistic-locking CAS
// actually rejects a stale write against real Postgres: two UpdateRole calls
// both read version 1, the first commits (-> version 2), the second — still
// holding version 1 — must be told the row moved to version 2.
func TestRolesService_Postgres_VersionConflict(t *testing.T) {
	db := pgtest.Open(t)
	_, merchantID := seedRolesTestMerchant(t, db, "lot6-verconflict")
	svc := newRolesTestService(db, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "itest-admin", MerchantID: merchantID})

	role, err := svc.CreateRole(ctx, CreateRoleRequest{Name: "Role A"})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}

	if _, err := svc.UpdateRole(ctx, role.ID, UpdateRoleRequest{Name: strPtr("Role A v2"), Version: role.Version}); err != nil {
		t.Fatalf("first UpdateRole (should win): %v", err)
	}

	_, err = svc.UpdateRole(ctx, role.ID, UpdateRoleRequest{Name: strPtr("Role A v2 (stale)"), Version: role.Version})
	var versionErr *VersionConflictError
	if !errors.As(err, &versionErr) {
		t.Fatalf("expected the second, stale-version UpdateRole to conflict, got %v", err)
	}
	if versionErr.CurrentVersion != 2 {
		t.Fatalf("expected current version 2, got %d", versionErr.CurrentVersion)
	}
}

// TestRolesService_Postgres_CrossTenantIsolation proves the SERVICE layer
// (not just the raw SQL invariant in TestNoCrossTenantRoleAssignment) never
// lets merchant A read or write merchant B's role — the id read as
// not-found, matching §1.
func TestRolesService_Postgres_CrossTenantIsolation(t *testing.T) {
	db := pgtest.Open(t)
	_, merchantA := seedRolesTestMerchant(t, db, "lot6-tenant-a")
	_, merchantB := seedRolesTestMerchant(t, db, "lot6-tenant-b")
	svc := newRolesTestService(db, nil)

	ctxB := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "itest-admin-b", MerchantID: merchantB})
	roleB, err := svc.CreateRole(ctxB, CreateRoleRequest{Name: "Role of B"})
	if err != nil {
		t.Fatalf("CreateRole (merchant B): %v", err)
	}

	ctxA := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "itest-admin-a", MerchantID: merchantA})
	if _, err := svc.GetRole(ctxA, roleB.ID); !errors.Is(err, models.ErrRoleNotFound) {
		t.Fatalf("GetRole across tenants: expected ErrRoleNotFound, got %v", err)
	}
	if _, err := svc.ReplacePermissions(ctxA, roleB.ID, ReplacePermissionsRequest{PermissionKeys: []permission.Key{permission.CatalogManage}, Version: 1}); !errors.Is(err, models.ErrRoleNotFound) {
		t.Fatalf("ReplacePermissions across tenants: expected ErrRoleNotFound, got %v", err)
	}
	if _, err := svc.ArchiveRole(ctxA, roleB.ID); !errors.Is(err, models.ErrRoleNotFound) {
		t.Fatalf("ArchiveRole across tenants: expected ErrRoleNotFound, got %v", err)
	}
}

// TestRolesService_Postgres_SetUserRole_NullRoleID is the real-world case the
// lot 4 runbook flagged: an orphaned users_rights row with role_id still
// NULL. PUT /users/{id}/role must succeed and set it, same as for any other
// row.
func TestRolesService_Postgres_SetUserRole_NullRoleID(t *testing.T) {
	db := pgtest.Open(t)
	_, merchantID := seedRolesTestMerchant(t, db, "lot6-nullrole")
	svc := newRolesTestService(db, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "itest-admin", MerchantID: merchantID})

	role, err := svc.CreateRole(ctx, CreateRoleRequest{Name: "Target role"})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	seedRolesTestUser(t, db, "itest-lot6-nullrole-user", merchantID, "", "itest-lot6-nullrole-tok")

	got, err := svc.SetUserRole(ctx, "itest-lot6-nullrole-user", SetUserRoleRequest{RoleID: role.ID})
	if err != nil {
		t.Fatalf("SetUserRole on a NULL role_id row: %v", err)
	}
	if got.ID != role.ID {
		t.Fatalf("unexpected role: %+v", got)
	}

	var roleID sql.NullString
	if err := db.QueryRowContext(context.Background(), `SELECT role_id FROM users_rights WHERE merchant_id = $1 AND user_id = $2`, merchantID, "itest-lot6-nullrole-user").Scan(&roleID); err != nil {
		t.Fatalf("read back role_id: %v", err)
	}
	if !roleID.Valid || roleID.String != role.ID {
		t.Fatalf("expected role_id = %q, got %+v", role.ID, roleID)
	}
}

// redisFromEnv returns a real *redisclient.Client against REDIS_URL, or skips
// the test if it is unset — same pattern as pgtest.Open for POSTGRES_URL.
func redisFromEnv(t *testing.T) *redisclient.Client {
	t.Helper()
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set — skipping cache-invalidation integration test")
	}
	client, err := redisclient.NewFromURL(url)
	if err != nil {
		t.Fatalf("connect to redis: %v", err)
	}
	return client
}

// TestRolesService_Postgres_CacheInvalidation is the §3-mandated integration
// test: seed a cached session, change the role's permissions, verify the key
// is gone and the next read reflects the new grants. Needs both POSTGRES_URL
// and REDIS_URL — skips (not fails) if either is absent, so this suite still
// runs everywhere else without a live Redis.
func TestRolesService_Postgres_CacheInvalidation(t *testing.T) {
	db := pgtest.Open(t)
	redis := redisFromEnv(t)
	_, merchantID := seedRolesTestMerchant(t, db, "lot6-cacheinval")
	svc := newRolesTestService(db, redis)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "itest-admin", MerchantID: merchantID})

	role, err := svc.CreateRole(ctx, CreateRoleRequest{Name: "Cached role"})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	const token = "itest-lot6-cache-tok"
	seedRolesTestUser(t, db, "itest-lot6-cache-user", merchantID, role.ID, token)

	cacheKey := models.UserCachePrefix + token
	if !redis.Set(context.Background(), cacheKey, `{"stale":"session"}`, time.Minute) {
		t.Fatal("failed to seed the cache key for this test")
	}
	t.Cleanup(func() { redis.Delete(context.Background(), cacheKey) })

	if _, err := svc.ReplacePermissions(ctx, role.ID, ReplacePermissionsRequest{
		PermissionKeys: []permission.Key{permission.CatalogManage}, Version: role.Version,
	}); err != nil {
		t.Fatalf("ReplacePermissions: %v", err)
	}

	if _, found := redis.Get(context.Background(), cacheKey); found {
		t.Fatal("expected the cached session to be invalidated after the role's permissions changed, but the key is still present")
	}
}
