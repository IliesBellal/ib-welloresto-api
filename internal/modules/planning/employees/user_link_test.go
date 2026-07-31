package employees

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

func TestServiceLinkEmployeeUserRejectsUserNotLinkedToMerchant(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT e.id, e.merchant_id, e.user_id, e.first_name, e.last_name, e.position_id, COALESCE(p.label, ''), e.position_note, e.email, e.phone, e.role,`)).
		WithArgs("merchant_1", "emp_1").
		WillReturnRows(employeeRows().AddRow(
			"emp_1", "merchant_1", nil, "John", "Doe", "pos_1", "Serveur", nil, nil, nil, "employee",
			"cdi", nil, nil, nil, nil,
			35.0, 35.0, 2, false, false,
			0, 0, 45.0, 0, nil, nil, nil,
			nil, nil, true, time.Now().UTC(), time.Now().UTC(), nil,
		))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(1)
		FROM users_rights ur`)).
		WithArgs("merchant_1", "user_9").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	_, err = svc.LinkEmployeeUser(ctx, "emp_1", EmployeeUserLinkRequest{UserID: "user_9"})
	if !errors.Is(err, models.ErrPlanningEmployeeUserNotLinkedToMerchant) {
		t.Fatalf("LinkEmployeeUser() error = %v, want %v", err, models.ErrPlanningEmployeeUserNotLinkedToMerchant)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestHandlerLinkEmployeeUserSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo)
	handler := NewHandler(svc)
	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT e.id, e.merchant_id, e.user_id, e.first_name, e.last_name, e.position_id, COALESCE(p.label, ''), e.position_note, e.email, e.phone, e.role,`)).
		WithArgs("merchant_1", "emp_1").
		WillReturnRows(employeeRows().AddRow(
			"emp_1", "merchant_1", nil, "John", "Doe", "pos_1", "Serveur", nil, nil, nil, "employee",
			"cdi", nil, nil, nil, nil,
			35.0, 35.0, 2, false, false,
			0, 0, 45.0, 0, nil, nil, nil,
			nil, nil, true, now, now, nil,
		))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(1)
		FROM users_rights ur`)).
		WithArgs("merchant_1", "user_9").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT e.id, e.merchant_id, e.user_id, e.first_name, e.last_name, e.position_id, COALESCE(p.label, ''), e.position_note, e.email, e.phone, e.role,`)).
		WithArgs("merchant_1", "user_9").
		WillReturnError(sql.ErrNoRows)

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE employees
		SET user_id = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
	`)).
		WithArgs("user_9", sqlmock.AnyArg(), "merchant_1", "emp_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT e.id, e.merchant_id, e.user_id, e.first_name, e.last_name, e.position_id, COALESCE(p.label, ''), e.position_note, e.email, e.phone, e.role,`)).
		WithArgs("merchant_1", "emp_1").
		WillReturnRows(employeeRows().AddRow(
			"emp_1", "merchant_1", "user_9", "John", "Doe", "pos_1", "Serveur", nil, nil, nil, "employee",
			"cdi", nil, nil, nil, nil,
			35.0, 35.0, 2, false, false,
			0, 0, 45.0, 0, nil, nil, nil,
			nil, nil, true, now, now, nil,
		))

	body, _ := json.Marshal(EmployeeUserLinkRequest{UserID: "user_9"})
	req := httptest.NewRequest(http.MethodPost, "/planning/employees/emp_1/user-link", bytes.NewReader(body))
	req = req.WithContext(middleware.WithUser(req.Context(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"}))
	req = withEmployeeRouteParam(req, "id", "emp_1")
	rec := httptest.NewRecorder()

	handler.LinkEmployeeUser(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("LinkEmployeeUser() status = %d, want %d", rec.Code, http.StatusOK)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestHandlerUnlinkEmployeeUserSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo)
	handler := NewHandler(svc)
	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT e.id, e.merchant_id, e.user_id, e.first_name, e.last_name, e.position_id, COALESCE(p.label, ''), e.position_note, e.email, e.phone, e.role,`)).
		WithArgs("merchant_1", "emp_1").
		WillReturnRows(employeeRows().AddRow(
			"emp_1", "merchant_1", "user_9", "John", "Doe", "pos_1", "Serveur", nil, nil, nil, "employee",
			"cdi", nil, nil, nil, nil,
			35.0, 35.0, 2, false, false,
			0, 0, 45.0, 0, nil, nil, nil,
			nil, nil, true, now, now, nil,
		))

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE employees
		SET user_id = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
	`)).
		WithArgs(nil, sqlmock.AnyArg(), "merchant_1", "emp_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT e.id, e.merchant_id, e.user_id, e.first_name, e.last_name, e.position_id, COALESCE(p.label, ''), e.position_note, e.email, e.phone, e.role,`)).
		WithArgs("merchant_1", "emp_1").
		WillReturnRows(employeeRows().AddRow(
			"emp_1", "merchant_1", nil, "John", "Doe", "pos_1", "Serveur", nil, nil, nil, "employee",
			"cdi", nil, nil, nil, nil,
			35.0, 35.0, 2, false, false,
			0, 0, 45.0, 0, nil, nil, nil,
			nil, nil, true, now, now, nil,
		))

	req := httptest.NewRequest(http.MethodDelete, "/planning/employees/emp_1/user-link", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"}))
	req = withEmployeeRouteParam(req, "id", "emp_1")
	rec := httptest.NewRecorder()

	handler.UnlinkEmployeeUser(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("UnlinkEmployeeUser() status = %d, want %d", rec.Code, http.StatusOK)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func withEmployeeRouteParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func employeeRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "merchant_id", "user_id", "first_name", "last_name", "position_id", "position", "position_note", "email", "phone", "role",
		"contract_type_code", "contract_start_date", "contract_end_date", "probation_end_date", "last_medical_checkup_date",
		"contract_hours", "max_weekly_hours", "required_rest_days", "sunday_premium", "night_premium",
		"hourly_rate", "gross_monthly_salary", "employer_charges_pct", "transport_cost", "birth_date", "gender", "nationality",
		"address", "hr_comment", "active", "created_at", "updated_at", "deleted_at",
	})
}
