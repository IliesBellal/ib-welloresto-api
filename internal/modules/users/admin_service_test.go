package users

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	planningemployees "welloresto-api/internal/modules/planning/employees"
)

func TestUsersHandlerListMerchantUsers(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil, nil, nil)
	handler := NewUsersHandler(svc, nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(1)
		FROM users_rights ur`)).
		WithArgs("merchant_1", "%jo%", "%jo%", "%jo%", "%jo%", true).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	createdAt := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)
	lastLoginAt := time.Date(2026, 5, 28, 11, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
			u.user_id,`)).
		WithArgs("merchant_1", "%jo%", "%jo%", "%jo%", "%jo%", true, 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "first_name", "last_name", "email", "tel", "profile_picture", "created_at", "last_login_at", "enabled", "login_enabled", "rights_id", "admin",
			"access_reception", "access_delivery", "access_waiter", "print_cash_report", "open_cash_drawer", "manage_menu", "manage_plannings", "manage_users", "manage_settings", "manage_haccp", "view_reports", "export_reports", "view_financials", "export_financials", "manage_customers", "export_customers", "employee_id", "employee_name",
		}).AddRow(
			"user_1", "John", "Doe", "john@example.com", "+33123456789", "https://cdn/avatar.png", createdAt, lastLoginAt, true, true, 12, true,
			true, false, false, true, true, true, true, true, false, false, true, false, false, false, false, false, "emp_1", "John Doe",
		))

	req := httptest.NewRequest(http.MethodGet, "/users?search=jo&active=true&linked_employee=true&page=1&page_size=20", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"}))
	rec := httptest.NewRecorder()

	handler.ListMerchantUsers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ListMerchantUsers() status = %d, want %d", rec.Code, http.StatusOK)
	}

	var response struct {
		ID   string `json:"id"`
		Data struct {
			Status string                 `json:"status"`
			Users  []MerchantUserListItem `json:"users"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != "users.list" {
		t.Fatalf("response ID = %q, want users.list", response.ID)
	}
	if len(response.Data.Users) != 1 {
		t.Fatalf("users length = %d, want 1", len(response.Data.Users))
	}
	if response.Data.Users[0].Permissions.ManageUsers != true {
		t.Fatalf("manage_users = %v, want true", response.Data.Users[0].Permissions.ManageUsers)
	}
	if response.Data.Users[0].LoginEnabled != true {
		t.Fatalf("login_enabled = %v, want true", response.Data.Users[0].LoginEnabled)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUsersHandlerSearchLinkableUsers(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil, nil, nil)
	handler := NewUsersHandler(svc, nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(1)
		FROM users u`)).
		WithArgs("merchant_1", "%al%", "%al%", "%al%", "%al%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	createdAt := time.Date(2026, 5, 28, 8, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT u.user_id, u.first_name, u.last_name, u.email, u.tel, u.profile_picture, u.created_at, u.last_login_at, u.enabled`)).
		WithArgs("merchant_1", "%al%", "%al%", "%al%", "%al%", 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "first_name", "last_name", "email", "tel", "profile_picture", "created_at", "last_login_at", "enabled"}).AddRow("user_2", "Alice", "Smith", "alice@example.com", "+33000000000", nil, createdAt, nil, true))

	req := httptest.NewRequest(http.MethodGet, "/users/linkable-search?search=al", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"}))
	rec := httptest.NewRecorder()

	handler.SearchLinkableUsers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("SearchLinkableUsers() status = %d, want %d", rec.Code, http.StatusOK)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUsersServiceForceResetPassword(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil, nil, nil)
	ctx := middleware.WithUser(testingContext(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
			id,
			merchant_id,`)).
		WithArgs("merchant_1", "user_9").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "merchant_id", "user_id", "admin", "login_enabled", "access_reception", "access_delivery", "access_waiter", "print_cash_report", "open_cash_drawer", "manage_menu", "manage_plannings", "manage_users", "manage_settings", "manage_haccp", "view_reports", "export_reports", "view_financials", "export_financials", "manage_customers", "export_customers",
		}).AddRow(77, "merchant_1", "user_9", false, true, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT token
		FROM users_rights
		WHERE merchant_id = ? AND user_id = ? AND enabled = TRUE
		LIMIT 1
	`)).
		WithArgs("merchant_1", "user_9").
		WillReturnRows(sqlmock.NewRows([]string{"token"}).AddRow("old_token_abc"))

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET password = ?, token = ?
		WHERE user_id = ?
	`)).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "user_9").
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users_rights
		SET token = ?
		WHERE user_id = ? AND merchant_id = ?
	`)).
		WithArgs(sqlmock.AnyArg(), "user_9", "merchant_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// The user's links on OTHER merchants must be rotated too: the password is
	// global, so no session may survive it (docs/PASSWORD_RESET.md, D10).
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, token FROM users_rights WHERE user_id = ? AND merchant_id <> ?`)).
		WithArgs("user_9", "merchant_1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "token"}).AddRow("88", "other_merchant_token"))

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users_rights SET token = ? WHERE id = ?`)).
		WithArgs(sqlmock.AnyArg(), "88").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := svc.ForceResetPassword(ctx, "user_9", ForceResetPasswordRequest{NewPassword: "NouveauPass123"}); err != nil {
		t.Fatalf("ForceResetPassword() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUsersHandlerUpdateMerchantUserRights(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil, nil, nil)
	handler := NewUsersHandler(svc, nil)

	body, _ := json.Marshal(MerchantUserRightsUpsertRequest{
		Admin: true,
		Permissions: MerchantUserPermissions{
			ManageUsers:     true,
			ManagePlannings: true,
		},
	})

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users_rights
		SET admin = ?,`)).
		WithArgs(true, false, false, false, false, false, false, true, true, false, false, false, false, false, false, false, false, false, "merchant_1", "user_4").
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT token
		FROM users_rights`)).
		WithArgs("merchant_1", "user_4").
		WillReturnRows(sqlmock.NewRows([]string{"token"}).AddRow("tok-user-4"))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT
			id,
			merchant_id,`)).
		WithArgs("merchant_1", "user_4").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "merchant_id", "user_id", "admin", "login_enabled", "access_reception", "access_delivery", "access_waiter", "print_cash_report", "open_cash_drawer", "manage_menu", "manage_plannings", "manage_users", "manage_settings", "manage_haccp", "view_reports", "export_reports", "view_financials", "export_financials", "manage_customers", "export_customers",
		}).AddRow(11, "merchant_1", "user_4", true, true, false, false, false, false, false, false, true, true, false, false, false, false, false, false, false, false))

	req := httptest.NewRequest(http.MethodPut, "/users/user_4/rights", bytes.NewReader(body))
	req = req.WithContext(middleware.WithUser(req.Context(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"}))
	rec := httptest.NewRecorder()
	req = withChiParam(req, "id", "user_4")

	handler.UpdateMerchantUserRights(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("UpdateMerchantUserRights() status = %d, want %d", rec.Code, http.StatusOK)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

// TestUsersServiceUnlinkMerchantUserLooksUpTokenBeforeDisabling verifies that
// UnlinkMerchantUser fetches the users_rights token (needed to invalidate the
// cached session) before DisableMerchantUserLink flips enabled to FALSE —
// GetUsersRightsToken filters on enabled=TRUE, so looking it up afterwards
// would always miss. svc.redis is nil here (as in the other service-level
// tests in this file): redis.Delete on a nil *redis.Client is a documented
// no-op, so this test exercises the call ordering and nil-safety, not the
// actual Redis DELETE payload.
func TestUsersServiceUnlinkMerchantUserLooksUpTokenBeforeDisabling(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil, nil, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT
			id,
			merchant_id,`)).
		WithArgs("merchant_1", "user_4").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "merchant_id", "user_id", "admin", "login_enabled", "access_reception", "access_delivery", "access_waiter", "print_cash_report", "open_cash_drawer", "manage_menu", "manage_plannings", "manage_users", "manage_settings", "manage_haccp", "view_reports", "export_reports", "view_financials", "export_financials", "manage_customers", "export_customers",
		}).AddRow(11, "merchant_1", "user_4", false, true, true, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT token
		FROM users_rights`)).
		WithArgs("merchant_1", "user_4").
		WillReturnRows(sqlmock.NewRows([]string{"token"}).AddRow("tok-user-4"))

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE employees
		SET user_id = NULL`)).
		WithArgs("merchant_1", "user_4").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users_rights
		SET enabled = FALSE`)).
		WithArgs("merchant_1", "user_4").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := svc.UnlinkMerchantUser(ctx, "user_4")
	if err != nil {
		t.Fatalf("UnlinkMerchantUser() error = %v", err)
	}
	if !result.Unlinked {
		t.Fatalf("UnlinkMerchantUser() result.Unlinked = false, want true")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUsersServiceLinkMerchantUserRejectsAlreadyLinked(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil, nil, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(1) FROM users WHERE user_id = ?`)).
		WithArgs("user_7").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(1)
		FROM users_rights
		WHERE merchant_id = ? AND user_id = ? AND enabled = TRUE
	`)).
		WithArgs("merchant_1", "user_7").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	_, err = svc.LinkMerchantUser(ctx, "user_7", MerchantUserLinkRequest{})
	if !errors.Is(err, models.ErrMerchantUserAlreadyLinked) {
		t.Fatalf("LinkMerchantUser() error = %v, want %v", err, models.ErrMerchantUserAlreadyLinked)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUsersServiceGetMerchantUserRightsNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil, nil, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT
			id,
			merchant_id,`)).
		WithArgs("merchant_1", "missing_user").
		WillReturnError(sql.ErrNoRows)

	_, err = svc.GetMerchantUserRights(ctx, "missing_user")
	if !errors.Is(err, models.ErrMerchantUserNotFound) {
		t.Fatalf("GetMerchantUserRights() error = %v, want %v", err, models.ErrMerchantUserNotFound)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func withChiParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func testingContext() context.Context {
	return context.Background()
}

type memberEmployeeStub struct {
	getActiveByUserIDFn func(ctx context.Context, merchantID, userID string) (*planningemployees.Employee, error)
	createEmployeeFn    func(ctx context.Context, req planningemployees.EmployeeCreateRequest) (*planningemployees.Employee, error)
	linkEmployeeUserFn  func(ctx context.Context, employeeID string, req planningemployees.EmployeeUserLinkRequest) (*planningemployees.Employee, error)
	updateEmployeeFn    func(ctx context.Context, employeeID string, req planningemployees.EmployeeUpdateRequest) (*planningemployees.Employee, error)
}

func (s *memberEmployeeStub) GetActiveEmployeeByUserID(ctx context.Context, merchantID, userID string) (*planningemployees.Employee, error) {
	if s.getActiveByUserIDFn != nil {
		return s.getActiveByUserIDFn(ctx, merchantID, userID)
	}
	return nil, nil
}

func (s *memberEmployeeStub) CreateEmployee(ctx context.Context, req planningemployees.EmployeeCreateRequest) (*planningemployees.Employee, error) {
	if s.createEmployeeFn != nil {
		return s.createEmployeeFn(ctx, req)
	}
	return nil, nil
}

func (s *memberEmployeeStub) LinkEmployeeUser(ctx context.Context, employeeID string, req planningemployees.EmployeeUserLinkRequest) (*planningemployees.Employee, error) {
	if s.linkEmployeeUserFn != nil {
		return s.linkEmployeeUserFn(ctx, employeeID, req)
	}
	return nil, nil
}

func (s *memberEmployeeStub) UpdateEmployee(ctx context.Context, employeeID string, req planningemployees.EmployeeUpdateRequest) (*planningemployees.Employee, error) {
	if s.updateEmployeeFn != nil {
		return s.updateEmployeeFn(ctx, employeeID, req)
	}
	return nil, nil
}

func TestUsersServiceGetMerchantUserMemberNilWhenNotLinked(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil, nil, nil)
	svc.memberEmployee = &memberEmployeeStub{
		getActiveByUserIDFn: func(ctx context.Context, merchantID, userID string) (*planningemployees.Employee, error) {
			return nil, sql.ErrNoRows
		},
	}

	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
			u.user_id,`)).
		WithArgs("merchant_1", "user_1").
		WillReturnRows(merchantUserDetailRows("user_1", "John", "Doe"))

	member, err := svc.GetMerchantUserMember(ctx, "user_1")
	if err != nil {
		t.Fatalf("GetMerchantUserMember() error = %v", err)
	}
	if member != nil {
		t.Fatalf("GetMerchantUserMember() = %#v, want nil", member)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUsersServicePatchMerchantUserMemberCreateRequiresPositionAndContract(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil, nil, nil)
	svc.memberEmployee = &memberEmployeeStub{
		getActiveByUserIDFn: func(ctx context.Context, merchantID, userID string) (*planningemployees.Employee, error) {
			return nil, sql.ErrNoRows
		},
	}

	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
			u.user_id,`)).
		WithArgs("merchant_1", "user_1").
		WillReturnRows(merchantUserDetailRows("user_1", "John", "Doe"))
	mock.ExpectBegin()
	mock.ExpectRollback()

	_, err = svc.PatchMerchantUserMember(ctx, "user_1", MerchantUserMemberPatchRequest{ContractTypeCode: stringPtr("cdi")})
	if !errors.Is(err, models.ErrPlanningEmployeePositionRequired) {
		t.Fatalf("PatchMerchantUserMember() error = %v, want %v", err, models.ErrPlanningEmployeePositionRequired)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUsersServicePatchMerchantUserMemberCreateAndUpdateFlow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil, nil, nil)

	callOrder := make([]string, 0, 4)
	svc.memberEmployee = &memberEmployeeStub{
		getActiveByUserIDFn: func(ctx context.Context, merchantID, userID string) (*planningemployees.Employee, error) {
			callOrder = append(callOrder, "get")
			return nil, sql.ErrNoRows
		},
		createEmployeeFn: func(ctx context.Context, req planningemployees.EmployeeCreateRequest) (*planningemployees.Employee, error) {
			callOrder = append(callOrder, "create")
			if req.FirstName != "John" || req.LastName != "Doe" {
				t.Fatalf("unexpected identity in create request: %#v", req)
			}
			return &planningemployees.Employee{ID: "emp_1"}, nil
		},
		linkEmployeeUserFn: func(ctx context.Context, employeeID string, req planningemployees.EmployeeUserLinkRequest) (*planningemployees.Employee, error) {
			callOrder = append(callOrder, "link")
			if employeeID != "emp_1" || req.UserID != "user_1" {
				t.Fatalf("unexpected link request: employee=%s req=%#v", employeeID, req)
			}
			return &planningemployees.Employee{ID: "emp_1"}, nil
		},
		updateEmployeeFn: func(ctx context.Context, employeeID string, req planningemployees.EmployeeUpdateRequest) (*planningemployees.Employee, error) {
			callOrder = append(callOrder, "update")
			if employeeID != "emp_1" {
				t.Fatalf("unexpected update employee ID: %s", employeeID)
			}
			if req.HourlyRate == nil || *req.HourlyRate != 1299 {
				t.Fatalf("hourly_rate not propagated in cents: %#v", req.HourlyRate)
			}
			return &planningemployees.Employee{ID: "emp_1", PositionID: "pos_1", Role: "manager", ContractTypeCode: "cdi", HourlyRate: 1299}, nil
		},
	}

	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
			u.user_id,`)).
		WithArgs("merchant_1", "user_1").
		WillReturnRows(merchantUserDetailRows("user_1", "John", "Doe"))
	mock.ExpectBegin()
	mock.ExpectCommit()

	hourlyRate := int64(1299)
	member, err := svc.PatchMerchantUserMember(ctx, "user_1", MerchantUserMemberPatchRequest{
		PositionID:       stringPtr("pos_1"),
		ContractTypeCode: stringPtr("cdi"),
		Role:             stringPtr("manager"),
		HourlyRate:       &hourlyRate,
	})
	if err != nil {
		t.Fatalf("PatchMerchantUserMember() error = %v", err)
	}
	if member == nil || member.HourlyRate != 1299 {
		t.Fatalf("PatchMerchantUserMember() member = %#v, want hourly_rate=1299", member)
	}
	if got := strings.Join(callOrder, ","); got != "get,create,link,update" {
		t.Fatalf("call order = %s, want get,create,link,update", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUsersServicePatchMerchantUserMemberRollbackOnLinkFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil, nil, nil)
	svc.memberEmployee = &memberEmployeeStub{
		getActiveByUserIDFn: func(ctx context.Context, merchantID, userID string) (*planningemployees.Employee, error) {
			return nil, sql.ErrNoRows
		},
		createEmployeeFn: func(ctx context.Context, req planningemployees.EmployeeCreateRequest) (*planningemployees.Employee, error) {
			return &planningemployees.Employee{ID: "emp_1"}, nil
		},
		linkEmployeeUserFn: func(ctx context.Context, employeeID string, req planningemployees.EmployeeUserLinkRequest) (*planningemployees.Employee, error) {
			return nil, errors.New("link failed")
		},
	}

	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
			u.user_id,`)).
		WithArgs("merchant_1", "user_1").
		WillReturnRows(merchantUserDetailRows("user_1", "John", "Doe"))
	mock.ExpectBegin()
	mock.ExpectRollback()

	_, err = svc.PatchMerchantUserMember(ctx, "user_1", MerchantUserMemberPatchRequest{
		PositionID:       stringPtr("pos_1"),
		ContractTypeCode: stringPtr("cdi"),
	})
	if err == nil || err.Error() != "link failed" {
		t.Fatalf("PatchMerchantUserMember() error = %v, want link failed", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUsersHandlerPatchMerchantUserMemberAcceptsDateOnlyJSON(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil, nil, nil)

	var capturedDate *time.Time
	svc.memberEmployee = &memberEmployeeStub{
		getActiveByUserIDFn: func(ctx context.Context, merchantID, userID string) (*planningemployees.Employee, error) {
			return &planningemployees.Employee{ID: "emp_1", ContractTypeCode: "cdi", PositionID: "pos_1"}, nil
		},
		updateEmployeeFn: func(ctx context.Context, employeeID string, req planningemployees.EmployeeUpdateRequest) (*planningemployees.Employee, error) {
			capturedDate = req.ContractStartDate
			return &planningemployees.Employee{ID: "emp_1", PositionID: "pos_1", ContractTypeCode: "cdi", ContractStartDate: req.ContractStartDate}, nil
		},
	}
	handler := NewUsersHandler(svc, nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
			u.user_id,`)).
		WithArgs("merchant_1", "user_1").
		WillReturnRows(merchantUserDetailRows("user_1", "John", "Doe"))
	mock.ExpectBegin()
	mock.ExpectCommit()

	body := bytes.NewBufferString(`{"contract_start_date":"2026-06-01"}`)
	req := httptest.NewRequest(http.MethodPatch, "/users/user_1/member", body)
	req = withChiParam(req, "id", "user_1")
	req = req.WithContext(middleware.WithUser(req.Context(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"}))
	rec := httptest.NewRecorder()

	handler.PatchMerchantUserMember(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PatchMerchantUserMember() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if capturedDate == nil || capturedDate.Format("2006-01-02") != "2026-06-01" {
		t.Fatalf("captured contract_start_date = %#v, want 2026-06-01", capturedDate)
	}
	if capturedDate != nil {
		y, m, d := capturedDate.Date()
		if capturedDate.Hour() != 0 || capturedDate.Minute() != 0 || capturedDate.Second() != 0 || y != 2026 || m != 6 || d != 1 {
			t.Fatalf("captured date must stay pure day without shift, got %v", capturedDate)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUsersServicePatchMerchantUserMemberDateAbsentNotModified(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil, nil, nil)
	svc.memberEmployee = &memberEmployeeStub{
		getActiveByUserIDFn: func(ctx context.Context, merchantID, userID string) (*planningemployees.Employee, error) {
			return &planningemployees.Employee{ID: "emp_1", PositionID: "pos_1", ContractTypeCode: "cdi"}, nil
		},
		updateEmployeeFn: func(ctx context.Context, employeeID string, req planningemployees.EmployeeUpdateRequest) (*planningemployees.Employee, error) {
			if req.ContractStartDate != nil {
				t.Fatalf("contract_start_date should be nil when absent")
			}
			return &planningemployees.Employee{ID: "emp_1", PositionID: "pos_1", ContractTypeCode: "cdi"}, nil
		},
	}

	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
			u.user_id,`)).
		WithArgs("merchant_1", "user_1").
		WillReturnRows(merchantUserDetailRows("user_1", "John", "Doe"))
	mock.ExpectBegin()
	mock.ExpectCommit()

	_, err = svc.PatchMerchantUserMember(ctx, "user_1", MerchantUserMemberPatchRequest{Role: stringPtr("manager")})
	if err != nil {
		t.Fatalf("PatchMerchantUserMember() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUsersHandlerPatchMerchantUserMemberDateNullHandled(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil, nil, nil)
	svc.memberEmployee = &memberEmployeeStub{
		getActiveByUserIDFn: func(ctx context.Context, merchantID, userID string) (*planningemployees.Employee, error) {
			return &planningemployees.Employee{ID: "emp_1", PositionID: "pos_1", ContractTypeCode: "cdi"}, nil
		},
		updateEmployeeFn: func(ctx context.Context, employeeID string, req planningemployees.EmployeeUpdateRequest) (*planningemployees.Employee, error) {
			if req.ContractStartDate != nil {
				t.Fatalf("contract_start_date should be nil when JSON null is provided")
			}
			return &planningemployees.Employee{ID: "emp_1", PositionID: "pos_1", ContractTypeCode: "cdi"}, nil
		},
	}
	handler := NewUsersHandler(svc, nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
			u.user_id,`)).
		WithArgs("merchant_1", "user_1").
		WillReturnRows(merchantUserDetailRows("user_1", "John", "Doe"))
	mock.ExpectBegin()
	mock.ExpectCommit()

	body := bytes.NewBufferString(`{"contract_start_date":null,"role":"manager"}`)
	req := httptest.NewRequest(http.MethodPatch, "/users/user_1/member", body)
	req = withChiParam(req, "id", "user_1")
	req = req.WithContext(middleware.WithUser(req.Context(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"}))
	rec := httptest.NewRecorder()

	handler.PatchMerchantUserMember(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PatchMerchantUserMember() status = %d, want %d", rec.Code, http.StatusOK)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestUsersHandlerGetMerchantUserMemberSerializesDateOnly(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil, nil, nil)
	svc.memberEmployee = &memberEmployeeStub{
		getActiveByUserIDFn: func(ctx context.Context, merchantID, userID string) (*planningemployees.Employee, error) {
			start := time.Date(2026, 6, 1, 18, 42, 0, 0, time.FixedZone("UTC+3", 3*3600))
			return &planningemployees.Employee{ID: "emp_1", PositionID: "pos_1", ContractTypeCode: "cdi", ContractStartDate: &start}, nil
		},
	}
	handler := NewUsersHandler(svc, nil)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
			u.user_id,`)).
		WithArgs("merchant_1", "user_1").
		WillReturnRows(merchantUserDetailRows("user_1", "John", "Doe"))

	req := httptest.NewRequest(http.MethodGet, "/users/user_1/member", nil)
	req = withChiParam(req, "id", "user_1")
	req = req.WithContext(middleware.WithUser(req.Context(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"}))
	rec := httptest.NewRecorder()

	handler.GetMerchantUserMember(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GetMerchantUserMember() status = %d, want %d", rec.Code, http.StatusOK)
	}

	var response map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response error = %v", err)
	}
	data, _ := response["data"].(map[string]any)
	member, _ := data["member"].(map[string]any)
	if member["contract_start_date"] != "2026-06-01" {
		t.Fatalf("contract_start_date = %#v, want 2026-06-01", member["contract_start_date"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func merchantUserDetailRows(userID, firstName, lastName string) *sqlmock.Rows {
	now := time.Now().UTC()
	return sqlmock.NewRows([]string{
		"user_id", "first_name", "last_name", "email", "tel", "profile_picture", "created_at", "last_login_at", "enabled", "login_enabled", "rights_id", "admin",
		"access_reception", "access_delivery", "access_waiter", "print_cash_report", "open_cash_drawer", "manage_menu", "manage_plannings", "manage_users", "manage_settings", "manage_haccp", "view_reports", "export_reports", "view_financials", "export_financials", "manage_customers", "export_customers", "employee_id", "employee_name",
	}).AddRow(
		userID, firstName, lastName, "john@example.com", "+33000000000", nil, now, now, true, true, int64(1), false,
		false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, nil, nil,
	)
}

func stringPtr(value string) *string {
	return &value
}
