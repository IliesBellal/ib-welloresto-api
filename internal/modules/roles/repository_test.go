package roles

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"welloresto-api/internal/permission"

	"github.com/DATA-DOG/go-sqlmock"
)

func newMockRepo(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewRepository(db), mock
}

func TestListPermissions(t *testing.T) {
	repo, mock := newMockRepo(t)

	cols := []string{"key", "domain", "label", "description", "is_sensitive", "sort_order", "deprecated_at"}
	mock.ExpectQuery("SELECT key, domain, label, description, is_sensitive, sort_order, deprecated_at").
		WillReturnRows(sqlmock.NewRows(cols).
			AddRow(permission.POSTicketReopen, "pos", "Rouvrir un ticket clôturé", "", false, 20, nil).
			AddRow(permission.SettingsManage, "settings", "Paramétrer l'établissement", "", true, 140, nil))

	got, err := repo.ListPermissions(context.Background())
	if err != nil {
		t.Fatalf("ListPermissions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 permissions, got %d", len(got))
	}
	if got[0].Key != permission.POSTicketReopen || got[0].IsSensitive {
		t.Fatalf("unexpected first permission: %+v", got[0])
	}
	if got[1].Key != permission.SettingsManage || !got[1].IsSensitive {
		t.Fatalf("unexpected second permission: %+v", got[1])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// expectEnsureSystemRoleCreated wires the sqlmock expectations for one
// ensureSystemRole call that finds no existing role, creates one, finds it has
// no permission yet, and grants the given number of them.
func expectEnsureSystemRoleCreated(mock sqlmock.Sqlmock, permissionCount int) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id FROM roles WHERE merchant_id = \\? AND system_key = \\?").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO roles").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT permission_key FROM role_permissions WHERE role_id = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"permission_key"})) // freshly created: nothing granted yet
	for i := 0; i < permissionCount; i++ {
		mock.ExpectExec("INSERT INTO role_permissions").
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectCommit()
}

func TestEnsureSystemRoles_CreatesBothRolesWhenMissing(t *testing.T) {
	repo, mock := newMockRepo(t)

	expectEnsureSystemRoleCreated(mock, len(permission.All)) // admin: every permission
	expectEnsureSystemRoleCreated(mock, 0)                   // staff: none (RBAC lot 8 — see systemRolePermissions)

	adminID, staffID, err := repo.EnsureSystemRoles(context.Background(), "m-1")
	if err != nil {
		t.Fatalf("EnsureSystemRoles: %v", err)
	}
	if !strings.HasPrefix(adminID, "role-") {
		t.Fatalf("adminID %q does not have the role-<uuid> format", adminID)
	}
	if !strings.HasPrefix(staffID, "role-") {
		t.Fatalf("staffID %q does not have the role-<uuid> format", staffID)
	}
	if adminID == staffID {
		t.Fatalf("admin and staff role ids must differ, got the same id %q", adminID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// permissionRows builds the sqlmock rows a "SELECT permission_key FROM
// role_permissions" query would return for the given already-granted keys.
func permissionRows(keys ...permission.Key) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"permission_key"})
	for _, k := range keys {
		rows.AddRow(string(k))
	}
	return rows
}

func TestEnsureSystemRoles_IdempotentReturnsExistingIDs(t *testing.T) {
	repo, mock := newMockRepo(t)

	const existingAdminID = "role-existing-admin"
	const existingStaffID = "role-existing-staff"

	// Admin already exists and already carries its full expected permission
	// set: no INSERT, but it IS reconciled (the permission_key query runs).
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id FROM roles WHERE merchant_id = \\? AND system_key = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(existingAdminID))
	mock.ExpectQuery("SELECT permission_key FROM role_permissions WHERE role_id = \\?").
		WillReturnRows(permissionRows(permission.All...))
	mock.ExpectCommit()

	// Staff already exists: found and left alone. No permission_key query at
	// all — staff is never reconciled past creation.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id FROM roles WHERE merchant_id = \\? AND system_key = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(existingStaffID))
	mock.ExpectCommit()

	adminID, staffID, err := repo.EnsureSystemRoles(context.Background(), "m-1")
	if err != nil {
		t.Fatalf("EnsureSystemRoles: %v", err)
	}
	if adminID != existingAdminID {
		t.Fatalf("expected existing admin id %q, got %q", existingAdminID, adminID)
	}
	if staffID != existingStaffID {
		t.Fatalf("expected existing staff id %q, got %q", existingStaffID, staffID)
	}

	// ExpectationsWereMet fails if an unexpected INSERT was issued — this is
	// what actually proves idempotency at the unit level.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations (an unexpected write would show up here): %v", err)
	}
}

// TestEnsureSystemRoles_BackfillsMissingPermissionOnExistingRole is the direct
// unit-level proof of the lot 2 "rattrape les rôles déjà créés" requirement:
// an admin role created before pos.status.manage existed gets it granted on
// the next EnsureSystemRoles call, without anything else changing.
func TestEnsureSystemRoles_BackfillsMissingPermissionOnExistingRole(t *testing.T) {
	repo, mock := newMockRepo(t)

	const existingAdminID = "role-existing-admin"
	const existingStaffID = "role-existing-staff"

	everythingButStatusManage := make([]permission.Key, 0, len(permission.All)-1)
	for _, k := range permission.All {
		if k != permission.POSStatusManage {
			everythingButStatusManage = append(everythingButStatusManage, k)
		}
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id FROM roles WHERE merchant_id = \\? AND system_key = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(existingAdminID))
	mock.ExpectQuery("SELECT permission_key FROM role_permissions WHERE role_id = \\?").
		WillReturnRows(permissionRows(everythingButStatusManage...))
	mock.ExpectExec("INSERT INTO role_permissions").
		WithArgs(existingAdminID, string(permission.POSStatusManage)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Staff already exists too, missing the same key by the same logic — but
	// it must NOT be backfilled: found and left alone, no permission_key
	// query at all.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id FROM roles WHERE merchant_id = \\? AND system_key = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(existingStaffID))
	mock.ExpectCommit()

	adminID, staffID, err := repo.EnsureSystemRoles(context.Background(), "m-1")
	if err != nil {
		t.Fatalf("EnsureSystemRoles: %v", err)
	}
	if adminID != existingAdminID || staffID != existingStaffID {
		t.Fatalf("expected existing ids to be reused, got admin=%q staff=%q", adminID, staffID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations (the backfill insert did not happen as expected): %v", err)
	}
}

// TestEnsureSystemRoles_ExistingStaffRoleNeverReconciled is the direct
// unit-level proof of the lot 2.5 fix: once a staff role exists, no later
// EnsureSystemRoles call queries or writes role_permissions for it — a
// customized "Employé polyvalent" (permissions the merchant changed by hand)
// cannot be silently reset by a re-run (migration catch-up, restore, deploy
// script). Only the "SELECT id" lookup runs for staff; sqlmock's unmet-
// expectations check is what actually proves no permission_key traffic
// happened.
func TestEnsureSystemRoles_ExistingStaffRoleNeverReconciled(t *testing.T) {
	repo, mock := newMockRepo(t)

	const existingAdminID = "role-existing-admin"
	const existingStaffID = "role-existing-staff"

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id FROM roles WHERE merchant_id = \\? AND system_key = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(existingAdminID))
	mock.ExpectQuery("SELECT permission_key FROM role_permissions WHERE role_id = \\?").
		WillReturnRows(permissionRows(permission.All...))
	mock.ExpectCommit()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id FROM roles WHERE merchant_id = \\? AND system_key = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(existingStaffID))
	mock.ExpectCommit()

	_, staffID, err := repo.EnsureSystemRoles(context.Background(), "m-1")
	if err != nil {
		t.Fatalf("EnsureSystemRoles: %v", err)
	}
	if staffID != existingStaffID {
		t.Fatalf("expected existing staff id %q, got %q", existingStaffID, staffID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations (staff role_permissions was touched, it must not be): %v", err)
	}
}

func TestGetRoleByID_NotFoundReturnsNilNil(t *testing.T) {
	repo, mock := newMockRepo(t)

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-missing").
		WillReturnError(sql.ErrNoRows)

	role, err := repo.GetRoleByID(context.Background(), "m-1", "role-missing")
	if err != nil {
		t.Fatalf("expected no error for a missing role, got %v", err)
	}
	if role != nil {
		t.Fatalf("expected nil role, got %+v", role)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetRolePermissions(t *testing.T) {
	repo, mock := newMockRepo(t)

	cols := []string{"key", "domain", "label", "description", "is_sensitive", "sort_order", "deprecated_at"}
	mock.ExpectQuery("SELECT p.key, p.domain, p.label, p.description, p.is_sensitive, p.sort_order, p.deprecated_at").
		WithArgs("role-1").
		WillReturnRows(sqlmock.NewRows(cols).
			AddRow(permission.POSTicketReopen, "pos", "Rouvrir un ticket clôturé", "", true, 20, nil).
			AddRow(permission.POSRefund, "pos", "Rembourser une vente", "", true, 40, nil))

	got, err := repo.GetRolePermissions(context.Background(), "role-1")
	if err != nil {
		t.Fatalf("GetRolePermissions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 permissions, got %d", len(got))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
