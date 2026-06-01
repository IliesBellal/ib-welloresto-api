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
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

func TestUsersHandlerListMerchantUsers(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil)
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
	svc := NewUsersService(repo, nil)
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
	svc := NewUsersService(repo, nil)
	ctx := middleware.WithUser(testingContext(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
			id,
			merchant_id,`)).
		WithArgs("merchant_1", "user_9").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "merchant_id", "user_id", "admin", "login_enabled", "access_reception", "access_delivery", "access_waiter", "print_cash_report", "open_cash_drawer", "manage_menu", "manage_plannings", "manage_users", "manage_settings", "manage_haccp", "view_reports", "export_reports", "view_financials", "export_financials", "manage_customers", "export_customers",
		}).AddRow(77, "merchant_1", "user_9", false, true, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false))

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET password = ?, token = ?
		WHERE user_id = ?
	`)).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "user_9").
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id
		FROM users_rights
		WHERE user_id = ?
	`)).
		WithArgs("user_9").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(77).AddRow(78))

	mock.ExpectExec(regexp.QuoteMeta(`
			UPDATE users_rights
			SET token = ?
			WHERE id = ?
		`)).
		WithArgs(sqlmock.AnyArg(), int64(77)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`
			UPDATE users_rights
			SET token = ?
			WHERE id = ?
		`)).
		WithArgs(sqlmock.AnyArg(), int64(78)).
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
	svc := NewUsersService(repo, nil)
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
		WithArgs(true, false, false, false, false, false, false, true, true, false, false, false, false, false, false, false, false, "merchant_1", "user_4").
		WillReturnResult(sqlmock.NewResult(0, 1))

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

func TestUsersServiceLinkMerchantUserRejectsAlreadyLinked(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewUserRepository(db)
	svc := NewUsersService(repo, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(1) FROM users WHERE user_id = ?`)).
		WithArgs("user_7").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(1)
		FROM users_rights
		WHERE merchant_id = ? AND user_id = ? AND enabled = 1
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
	svc := NewUsersService(repo, nil)
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
