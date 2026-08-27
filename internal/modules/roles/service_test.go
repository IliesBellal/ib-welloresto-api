package roles

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	usersModule "welloresto-api/internal/modules/users"
	"welloresto-api/internal/permission"
)

func strPtr(s string) *string { return &s }

// newMockService builds a Service whose Repository and users.UsersRepository
// share the same sqlmock database, with audit and redis left nil (this
// package's tests only ever assert on the DB write sequence and the guard
// decisions — the same convention as users.admin_service_test.go, which also
// passes nil for both; genuine Redis interaction is covered by the
// postgres_integration-tagged test instead, since redisclient.Client's
// backing *redis.Client cannot be faked without a live server).
func newMockService(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewRepository(db)
	usersRepo := usersModule.NewUserRepository(db)
	return NewService(repo, usersRepo, nil, nil), mock
}

func ctxWithUser(u *auth.UserLoginRow) context.Context {
	return middleware.WithUser(context.Background(), u)
}

var fixedTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func roleRow(id, merchantID, name, description string, systemKey *string, version int, archivedAt *time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "merchant_id", "name", "description", "system_key", "version", "created_at", "updated_at", "archived_at"}).
		AddRow(id, merchantID, name, description, systemKey, version, fixedTime, fixedTime, archivedAt)
}

func permRows(keys ...permission.Key) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"key", "domain", "label", "description", "is_sensitive", "sort_order", "deprecated_at"})
	for i, k := range keys {
		rows.AddRow(string(k), "domain", "label", "", false, i*10, nil)
	}
	return rows
}

// ---------------------------------------------------------------------------
// GET /permissions, GET /me/permissions
// ---------------------------------------------------------------------------

func TestListPermissionCatalog_GroupsByDomain(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery("SELECT key, domain, label, description, is_sensitive, sort_order, deprecated_at").
		WillReturnRows(sqlmock.NewRows([]string{"key", "domain", "label", "description", "is_sensitive", "sort_order", "deprecated_at"}).
			AddRow(string(permission.POSTicketReopen), "pos", "l1", "", false, 10, nil).
			AddRow(string(permission.POSRefund), "pos", "l2", "", true, 40, nil).
			AddRow(string(permission.CatalogManage), "catalog", "l3", "", true, 60, nil))

	groups, err := svc.ListPermissionCatalog(context.Background())
	if err != nil {
		t.Fatalf("ListPermissionCatalog: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 domain groups, got %d: %+v", len(groups), groups)
	}
	if groups[0].Domain != "pos" || len(groups[0].Permissions) != 2 {
		t.Fatalf("unexpected pos group: %+v", groups[0])
	}
	if groups[1].Domain != "catalog" || len(groups[1].Permissions) != 1 {
		t.Fatalf("unexpected catalog group: %+v", groups[1])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMyPermissions_LegacyAdminGetsEverythingNoRole(t *testing.T) {
	svc, mock := newMockService(t)

	user := &auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1", Rights: auth.UserRowRights{Admin: true}}
	result, err := svc.MyPermissions(ctxWithUser(user))
	if err != nil {
		t.Fatalf("MyPermissions: %v", err)
	}
	if !result.IsAdmin {
		t.Fatal("expected IsAdmin = true for legacy Rights.Admin")
	}
	if result.Role != nil {
		t.Fatalf("expected nil role (RoleID nil), got %+v", result.Role)
	}
	if len(result.Permissions) != len(permission.All) {
		t.Fatalf("expected every catalog key granted, got %d/%d", len(result.Permissions), len(permission.All))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMyPermissions_RoleBasedStaffGetsOnlyGrantedKeys(t *testing.T) {
	svc, mock := newMockService(t)

	roleID := "role-staff-1"
	systemKey := permission.SystemKeyStaff
	user := &auth.UserLoginRow{
		UserID: "u-2", MerchantID: "m-1",
		RoleID: &roleID, RoleSystemKey: &systemKey,
		Permissions: []string{string(permission.POSTicketReopen)},
	}

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", roleID).
		WillReturnRows(roleRow(roleID, "m-1", "Employé polyvalent", "", &systemKey, 1, nil))

	result, err := svc.MyPermissions(ctxWithUser(user))
	if err != nil {
		t.Fatalf("MyPermissions: %v", err)
	}
	if result.IsAdmin {
		t.Fatal("expected IsAdmin = false for a staff role")
	}
	if result.Role == nil || result.Role.Name != "Employé polyvalent" {
		t.Fatalf("expected the staff role attached, got %+v", result.Role)
	}
	if len(result.Permissions) != 1 || result.Permissions[0] != permission.POSTicketReopen {
		t.Fatalf("expected exactly [pos.ticket.reopen], got %v", result.Permissions)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Cross-tenant isolation (§1: another merchant's role_id must 404, not 403)
// ---------------------------------------------------------------------------

func TestGetRole_CrossTenantRoleIsNotFound(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("merchant-A", "role-of-merchant-B").
		WillReturnError(sql.ErrNoRows)

	_, err := svc.GetRole(ctxWithUser(&auth.UserLoginRow{UserID: "u-1", MerchantID: "merchant-A"}), "role-of-merchant-B")
	if !errors.Is(err, models.ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Create / duplicate
// ---------------------------------------------------------------------------

func TestCreateRole_Plain(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO roles").
		WithArgs(sqlmock.AnyArg(), "m-1", "Chef de cuisine", "desc").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", sqlmock.AnyArg()).
		WillReturnRows(roleRow("role-new", "m-1", "Chef de cuisine", "desc", nil, 1, nil))
	mock.ExpectQuery("SELECT p.key, p.domain, p.label, p.description, p.is_sensitive, p.sort_order, p.deprecated_at").
		WillReturnRows(permRows())

	got, err := svc.CreateRole(ctxWithUser(&auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}), CreateRoleRequest{
		Name: "Chef de cuisine", Description: "desc",
	})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if got.Name != "Chef de cuisine" || len(got.Permissions) != 0 {
		t.Fatalf("unexpected role: %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateRole_EmptyNameRejected(t *testing.T) {
	svc, _ := newMockService(t)
	_, err := svc.CreateRole(ctxWithUser(&auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}), CreateRoleRequest{Name: "   "})
	if !errors.Is(err, models.ErrRoleNameRequired) {
		t.Fatalf("expected ErrRoleNameRequired, got %v", err)
	}
}

func TestCreateRole_DuplicatesSourcePermissions(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-source").
		WillReturnRows(roleRow("role-source", "m-1", "Source", "", nil, 2, nil))
	mock.ExpectQuery("SELECT p.key, p.domain, p.label, p.description, p.is_sensitive, p.sort_order, p.deprecated_at").
		WithArgs("role-source").
		WillReturnRows(permRows(permission.POSTicketReopen, permission.POSRefund))

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO roles").
		WithArgs(sqlmock.AnyArg(), "m-1", "Copy", "").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO role_permissions").
		WithArgs(sqlmock.AnyArg(), string(permission.POSTicketReopen)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO role_permissions").
		WithArgs(sqlmock.AnyArg(), string(permission.POSRefund)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", sqlmock.AnyArg()).
		WillReturnRows(roleRow("role-new", "m-1", "Copy", "", nil, 1, nil))
	mock.ExpectQuery("SELECT p.key, p.domain, p.label, p.description, p.is_sensitive, p.sort_order, p.deprecated_at").
		WillReturnRows(permRows(permission.POSTicketReopen, permission.POSRefund))

	got, err := svc.CreateRole(ctxWithUser(&auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}), CreateRoleRequest{
		Name: "Copy", DuplicateFromRoleID: strPtr("role-source"),
	})
	if err != nil {
		t.Fatalf("CreateRole (duplicate): %v", err)
	}
	if len(got.Permissions) != 2 {
		t.Fatalf("expected 2 duplicated permissions, got %d", len(got.Permissions))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Rename (PATCH) + version conflict
// ---------------------------------------------------------------------------

func TestUpdateRole_RenameSuccess(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-1").
		WillReturnRows(roleRow("role-1", "m-1", "Old name", "old desc", nil, 3, nil))

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("role-1").
		WillReturnRows(roleRow("role-1", "m-1", "Old name", "old desc", nil, 3, nil))
	mock.ExpectExec("UPDATE roles").
		WithArgs("New name", "old desc", "role-1", 3).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-1").
		WillReturnRows(roleRow("role-1", "m-1", "New name", "old desc", nil, 4, nil))
	mock.ExpectQuery("SELECT p.key, p.domain, p.label, p.description, p.is_sensitive, p.sort_order, p.deprecated_at").
		WillReturnRows(permRows())

	got, err := svc.UpdateRole(ctxWithUser(&auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}), "role-1", UpdateRoleRequest{
		Name: strPtr("New name"), Version: 3,
	})
	if err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	if got.Name != "New name" || got.Version != 4 {
		t.Fatalf("unexpected role after rename: %+v", got.Role)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateRole_VersionConflictReturnsCurrentVersion(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-1").
		WillReturnRows(roleRow("role-1", "m-1", "Name", "", nil, 3, nil))

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("role-1").
		WillReturnRows(roleRow("role-1", "m-1", "Name", "", nil, 3, nil))
	mock.ExpectExec("UPDATE roles").
		WithArgs("New name", "", "role-1", 3).
		WillReturnResult(sqlmock.NewResult(0, 0))

	// notFoundOrVersionConflict re-reads the current row.
	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("role-1").
		WillReturnRows(roleRow("role-1", "m-1", "Name", "", nil, 5, nil))

	_, err := svc.UpdateRole(ctxWithUser(&auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}), "role-1", UpdateRoleRequest{
		Name: strPtr("New name"), Version: 3,
	})
	var versionErr *VersionConflictError
	if !errors.As(err, &versionErr) {
		t.Fatalf("expected *VersionConflictError, got %v", err)
	}
	if versionErr.CurrentVersion != 5 {
		t.Fatalf("expected current version 5, got %d", versionErr.CurrentVersion)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateRole_MissingVersionRejected(t *testing.T) {
	svc, _ := newMockService(t)
	_, err := svc.UpdateRole(ctxWithUser(&auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}), "role-1", UpdateRoleRequest{Name: strPtr("X")})
	if !errors.Is(err, models.ErrRoleVersionRequired) {
		t.Fatalf("expected ErrRoleVersionRequired, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// G4: admin role is immutable
// ---------------------------------------------------------------------------

func TestUpdateRole_AdminRoleImmutable(t *testing.T) {
	svc, mock := newMockService(t)
	adminKey := permission.SystemKeyAdmin

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-admin").
		WillReturnRows(roleRow("role-admin", "m-1", "Administrateur", "", &adminKey, 1, nil))

	_, err := svc.UpdateRole(ctxWithUser(&auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}), "role-admin", UpdateRoleRequest{
		Name: strPtr("Hacked"), Version: 1,
	})
	if !errors.Is(err, models.ErrRoleImmutable) {
		t.Fatalf("expected ErrRoleImmutable, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestReplacePermissions_AdminRoleImmutable(t *testing.T) {
	svc, mock := newMockService(t)
	adminKey := permission.SystemKeyAdmin

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-admin").
		WillReturnRows(roleRow("role-admin", "m-1", "Administrateur", "", &adminKey, 1, nil))

	_, err := svc.ReplacePermissions(ctxWithUser(&auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}), "role-admin", ReplacePermissionsRequest{
		PermissionKeys: []permission.Key{}, Version: 1,
	})
	if !errors.Is(err, models.ErrRoleImmutable) {
		t.Fatalf("expected ErrRoleImmutable, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestArchiveRole_AdminRoleImmutable(t *testing.T) {
	svc, mock := newMockService(t)
	adminKey := permission.SystemKeyAdmin

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-admin").
		WillReturnRows(roleRow("role-admin", "m-1", "Administrateur", "", &adminKey, 1, nil))

	_, err := svc.ArchiveRole(ctxWithUser(&auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}), "role-admin")
	if !errors.Is(err, models.ErrRoleImmutable) {
		t.Fatalf("expected ErrRoleImmutable, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// G1: self-modification (own role assignment, own role's permissions)
// ---------------------------------------------------------------------------

func TestReplacePermissions_CannotEditOwnRole(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-mine").
		WillReturnRows(roleRow("role-mine", "m-1", "Mine", "", nil, 1, nil))

	user := &auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1", RoleID: strPtr("role-mine")}
	_, err := svc.ReplacePermissions(ctxWithUser(user), "role-mine", ReplacePermissionsRequest{
		PermissionKeys: permission.All, Version: 1,
	})
	if !errors.Is(err, models.ErrRoleSelfModification) {
		t.Fatalf("expected ErrRoleSelfModification, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestSetUserRole_CannotChangeOwnRole(t *testing.T) {
	svc, _ := newMockService(t)
	user := &auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}
	_, err := svc.SetUserRole(ctxWithUser(user), "u-1", SetUserRoleRequest{RoleID: "role-x"})
	if !errors.Is(err, models.ErrRoleSelfModification) {
		t.Fatalf("expected ErrRoleSelfModification, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// G2: last active staff.manage holder
// ---------------------------------------------------------------------------

func TestReplacePermissions_CannotStripLastStaffManageHolder(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-managers").
		WillReturnRows(roleRow("role-managers", "m-1", "Managers", "", nil, 2, nil))
	mock.ExpectQuery("SELECT p.key, p.domain, p.label, p.description, p.is_sensitive, p.sort_order, p.deprecated_at").
		WithArgs("role-managers").
		WillReturnRows(permRows(permission.StaffManage))
	mock.ExpectQuery("SELECT COUNT\\(DISTINCT ur.id\\)").
		WithArgs("m-1", string(permission.StaffManage), "role-managers").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	user := &auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1", RoleID: strPtr("role-other")}
	_, err := svc.ReplacePermissions(ctxWithUser(user), "role-managers", ReplacePermissionsRequest{
		PermissionKeys: []permission.Key{permission.POSTicketReopen}, Version: 2,
	})
	if !errors.Is(err, models.ErrRoleStaffManageRequired) {
		t.Fatalf("expected ErrRoleStaffManageRequired, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestSetUserRole_CannotMoveLastStaffManageHolderAway(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-staff").
		WillReturnRows(roleRow("role-staff", "m-1", "Staff", "", nil, 1, nil))
	mock.ExpectQuery("SELECT id, role_id FROM users_rights").
		WithArgs("m-1", "user-2").
		WillReturnRows(sqlmock.NewRows([]string{"id", "role_id"}).AddRow(9, "role-admins"))
	mock.ExpectQuery("SELECT p.key, p.domain, p.label, p.description, p.is_sensitive, p.sort_order, p.deprecated_at").
		WithArgs("role-admins").
		WillReturnRows(permRows(permission.StaffManage))
	mock.ExpectQuery("SELECT p.key, p.domain, p.label, p.description, p.is_sensitive, p.sort_order, p.deprecated_at").
		WithArgs("role-staff").
		WillReturnRows(permRows(permission.POSTicketReopen))
	mock.ExpectQuery("SELECT COUNT\\(DISTINCT ur.id\\)").
		WithArgs("m-1", string(permission.StaffManage)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	user := &auth.UserLoginRow{UserID: "admin-1", MerchantID: "m-1"}
	_, err := svc.SetUserRole(ctxWithUser(user), "user-2", SetUserRoleRequest{RoleID: "role-staff"})
	if !errors.Is(err, models.ErrRoleStaffManageRequired) {
		t.Fatalf("expected ErrRoleStaffManageRequired, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// SetUserRole: the real orphan-row case — target's role_id is currently NULL
// ---------------------------------------------------------------------------

func TestSetUserRole_TargetHadNullRoleID(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-staff").
		WillReturnRows(roleRow("role-staff", "m-1", "Staff", "", nil, 1, nil))
	mock.ExpectQuery("SELECT id, role_id FROM users_rights").
		WithArgs("m-1", "user-2").
		WillReturnRows(sqlmock.NewRows([]string{"id", "role_id"}).AddRow(9, nil))
	mock.ExpectExec("UPDATE users_rights SET role_id").
		WithArgs("role-staff", "m-1", "user-2").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT token FROM users_rights").
		WithArgs("m-1", "user-2").
		WillReturnRows(sqlmock.NewRows([]string{"token"}).AddRow("tok-2"))

	user := &auth.UserLoginRow{UserID: "admin-1", MerchantID: "m-1"}
	got, err := svc.SetUserRole(ctxWithUser(user), "user-2", SetUserRoleRequest{RoleID: "role-staff"})
	if err != nil {
		t.Fatalf("SetUserRole (orphan role_id NULL): %v", err)
	}
	if got.ID != "role-staff" {
		t.Fatalf("unexpected role: %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestSetUserRole_TargetNotFound(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-staff").
		WillReturnRows(roleRow("role-staff", "m-1", "Staff", "", nil, 1, nil))
	mock.ExpectQuery("SELECT id, role_id FROM users_rights").
		WithArgs("m-1", "user-ghost").
		WillReturnError(sql.ErrNoRows)

	user := &auth.UserLoginRow{UserID: "admin-1", MerchantID: "m-1"}
	_, err := svc.SetUserRole(ctxWithUser(user), "user-ghost", SetUserRoleRequest{RoleID: "role-staff"})
	if !errors.Is(err, models.ErrMerchantUserNotFound) {
		t.Fatalf("expected ErrMerchantUserNotFound, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// G5: cannot archive a role with any holder (enabled or not)
// ---------------------------------------------------------------------------

func TestArchiveRole_HasMembersReturnsCount(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-1").
		WillReturnRows(roleRow("role-1", "m-1", "Custom", "", nil, 1, nil))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM users_rights WHERE role_id = \\?").
		WithArgs("role-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	_, err := svc.ArchiveRole(ctxWithUser(&auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}), "role-1")
	var membersErr *RoleHasMembersError
	if !errors.As(err, &membersErr) {
		t.Fatalf("expected *RoleHasMembersError, got %v", err)
	}
	if membersErr.Count != 3 {
		t.Fatalf("expected count 3, got %d", membersErr.Count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// G6: staff role cannot be archived while it is the merchant default
// ---------------------------------------------------------------------------

func TestArchiveRole_StaffRoleStillDefaultRejected(t *testing.T) {
	svc, mock := newMockService(t)
	staffKey := permission.SystemKeyStaff

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-staff").
		WillReturnRows(roleRow("role-staff", "m-1", "Staff", "", &staffKey, 1, nil))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM users_rights WHERE role_id = \\?").
		WithArgs("role-staff").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("SELECT default_role_id FROM merchant").
		WithArgs("m-1").
		WillReturnRows(sqlmock.NewRows([]string{"default_role_id"}).AddRow("role-staff"))

	_, err := svc.ArchiveRole(ctxWithUser(&auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}), "role-staff")
	if !errors.Is(err, models.ErrRoleIsMerchantDefault) {
		t.Fatalf("expected ErrRoleIsMerchantDefault, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestArchiveRole_SuccessInvalidatesHolderTokens(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-1").
		WillReturnRows(roleRow("role-1", "m-1", "Custom", "", nil, 1, nil))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM users_rights WHERE role_id = \\?").
		WithArgs("role-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("SELECT token FROM users_rights WHERE role_id = \\? AND enabled = TRUE").
		WithArgs("role-1").
		WillReturnRows(sqlmock.NewRows([]string{"token"}))
	mock.ExpectQuery("SELECT p.key, p.domain, p.label, p.description, p.is_sensitive, p.sort_order, p.deprecated_at").
		WithArgs("role-1").
		WillReturnRows(permRows())
	mock.ExpectExec("UPDATE roles SET archived_at").
		WithArgs("role-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	archivedAt := fixedTime
	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-1").
		WillReturnRows(roleRow("role-1", "m-1", "Custom", "", nil, 1, &archivedAt))

	got, err := svc.ArchiveRole(ctxWithUser(&auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}), "role-1")
	if err != nil {
		t.Fatalf("ArchiveRole: %v", err)
	}
	if got.ArchivedAt == nil {
		t.Fatal("expected ArchivedAt to be set")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestArchiveRole_AlreadyArchivedIsIdempotent(t *testing.T) {
	svc, mock := newMockService(t)
	archivedAt := fixedTime

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-1").
		WillReturnRows(roleRow("role-1", "m-1", "Custom", "", nil, 1, &archivedAt))

	got, err := svc.ArchiveRole(ctxWithUser(&auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}), "role-1")
	if err != nil {
		t.Fatalf("ArchiveRole (already archived): %v", err)
	}
	if got.ArchivedAt == nil {
		t.Fatal("expected the already-archived role back unchanged")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ReplacePermissions: success path invalidates every holder's session
// ---------------------------------------------------------------------------

func TestReplacePermissions_SuccessInvalidatesHolderTokens(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-1").
		WillReturnRows(roleRow("role-1", "m-1", "Custom", "", nil, 2, nil))
	mock.ExpectQuery("SELECT p.key, p.domain, p.label, p.description, p.is_sensitive, p.sort_order, p.deprecated_at").
		WithArgs("role-1").
		WillReturnRows(permRows(permission.POSTicketReopen))
	mock.ExpectQuery("SELECT token FROM users_rights WHERE role_id = \\? AND enabled = TRUE").
		WithArgs("role-1").
		WillReturnRows(sqlmock.NewRows([]string{"token"}).AddRow("tok-1").AddRow("tok-2"))

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE roles").
		WithArgs("role-1", 2).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM role_permissions WHERE role_id = \\?").
		WithArgs("role-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO role_permissions").
		WithArgs("role-1", string(permission.CatalogManage)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-1").
		WillReturnRows(roleRow("role-1", "m-1", "Custom", "", nil, 3, nil))
	mock.ExpectQuery("SELECT p.key, p.domain, p.label, p.description, p.is_sensitive, p.sort_order, p.deprecated_at").
		WithArgs("role-1").
		WillReturnRows(permRows(permission.CatalogManage))

	user := &auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1", RoleID: strPtr("role-other")}
	got, err := svc.ReplacePermissions(ctxWithUser(user), "role-1", ReplacePermissionsRequest{
		PermissionKeys: []permission.Key{permission.CatalogManage}, Version: 2,
	})
	if err != nil {
		t.Fatalf("ReplacePermissions: %v", err)
	}
	if got.Version != 3 || len(got.Permissions) != 1 || got.Permissions[0].Key != permission.CatalogManage {
		t.Fatalf("unexpected result: %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestReplacePermissions_VersionConflictReturnsCurrentVersion(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-1").
		WillReturnRows(roleRow("role-1", "m-1", "Custom", "", nil, 2, nil))
	mock.ExpectQuery("SELECT p.key, p.domain, p.label, p.description, p.is_sensitive, p.sort_order, p.deprecated_at").
		WithArgs("role-1").
		WillReturnRows(permRows(permission.POSTicketReopen))
	mock.ExpectQuery("SELECT token FROM users_rights WHERE role_id = \\? AND enabled = TRUE").
		WithArgs("role-1").
		WillReturnRows(sqlmock.NewRows([]string{"token"}))

	// Someone else wrote to role-1 between this caller's GET and this PUT:
	// the CAS in ReplaceRolePermissions matches nothing. The transaction
	// still commits (nothing was written either way — the mismatch is
	// reported by returning matched=false, not by erroring the tx).
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE roles").
		WithArgs("role-1", 2).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("role-1").
		WillReturnRows(roleRow("role-1", "m-1", "Custom", "", nil, 6, nil))

	user := &auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1", RoleID: strPtr("role-other")}
	_, err := svc.ReplacePermissions(ctxWithUser(user), "role-1", ReplacePermissionsRequest{
		PermissionKeys: []permission.Key{permission.CatalogManage}, Version: 2,
	})
	var versionErr *VersionConflictError
	if !errors.As(err, &versionErr) {
		t.Fatalf("expected *VersionConflictError, got %v", err)
	}
	if versionErr.CurrentVersion != 6 {
		t.Fatalf("expected current version 6, got %d", versionErr.CurrentVersion)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestReplacePermissions_UnknownKeyRejected(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-1").
		WillReturnRows(roleRow("role-1", "m-1", "Custom", "", nil, 1, nil))

	user := &auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1", RoleID: strPtr("role-other")}
	_, err := svc.ReplacePermissions(ctxWithUser(user), "role-1", ReplacePermissionsRequest{
		PermissionKeys: []permission.Key{"not.a.real.key"}, Version: 1,
	})
	if !errors.Is(err, models.ErrRolePermissionKeyUnknown) {
		t.Fatalf("expected ErrRolePermissionKeyUnknown, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// SetMerchantDefaultRole
// ---------------------------------------------------------------------------

func TestSetMerchantDefaultRole_Success(t *testing.T) {
	svc, mock := newMockService(t)

	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("m-1", "role-new-default").
		WillReturnRows(roleRow("role-new-default", "m-1", "Admin", "", nil, 1, nil))
	mock.ExpectQuery("SELECT default_role_id FROM merchant").
		WithArgs("m-1").
		WillReturnRows(sqlmock.NewRows([]string{"default_role_id"}).AddRow("role-old-default"))
	mock.ExpectExec("UPDATE merchant SET default_role_id").
		WithArgs("role-new-default", "m-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, merchant_id, name, description, system_key, version, created_at, updated_at, archived_at").
		WithArgs("role-old-default").
		WillReturnRows(roleRow("role-old-default", "m-1", "Old default", "", nil, 1, nil))

	got, err := svc.SetMerchantDefaultRole(ctxWithUser(&auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}), SetDefaultRoleRequest{RoleID: "role-new-default"})
	if err != nil {
		t.Fatalf("SetMerchantDefaultRole: %v", err)
	}
	if got.ID != "role-new-default" {
		t.Fatalf("unexpected role: %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
