package timeentries

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/modules/auth"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	settingspkg "welloresto-api/internal/modules/planning/settings"
)

func TestHandlerStartCurrentUserTimeEntrySuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(
		repo,
		stubEmployeeReader{employee: &employeespkg.Employee{ID: "emp_1"}, memberEmployeeID: "emp_1"},
		nil,
		stubSettingsReader{settings: &settingspkg.PlanningSettings{AttendanceSource: settingspkg.AttendanceSourcePointage}},
		nil,
	)
	handler := NewHandler(svc)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, modified_by, modified_at, modification_reason, created_at, updated_at, deleted_at
		FROM planning_time_entries
		WHERE merchant_id = ? AND employee_id = ? AND clock_out_at IS NULL AND enabled = 1
		ORDER BY clock_in_at DESC, created_at DESC
		LIMIT 1
	`)).
		WithArgs("merchant_1", "emp_1").
		WillReturnError(sql.ErrNoRows)

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO planning_time_entries (
			id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, modified_by, modified_at, modification_reason, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`)).
		WithArgs(sqlmock.AnyArg(), "merchant_1", "emp_1", nil, "pointage", sqlmock.AnyArg(), nil, nil, nil, nil, nil, nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest(http.MethodPost, "/planning/me/time-entries/start", bytes.NewBufferString(`{}`))
	req = req.WithContext(middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1", MerchantRightsID: "42"}))
	rec := httptest.NewRecorder()

	handler.StartCurrentUserTimeEntry(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("StartCurrentUserTimeEntry() status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if !strings.Contains(rec.Body.String(), `"status":"success"`) {
		t.Fatalf("StartCurrentUserTimeEntry() body = %s, want success status", rec.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestHandlerStopCurrentUserTimeEntrySuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(
		repo,
		stubEmployeeReader{employee: &employeespkg.Employee{ID: "emp_1"}, memberEmployeeID: "emp_1"},
		nil,
		nil,
		nil,
	)
	handler := NewHandler(svc)
	now := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, modified_by, modified_at, modification_reason, created_at, updated_at, deleted_at
		FROM planning_time_entries
		WHERE merchant_id = ? AND employee_id = ? AND clock_out_at IS NULL AND enabled = 1
		ORDER BY clock_in_at DESC, created_at DESC
		LIMIT 1
	`)).
		WithArgs("merchant_1", "emp_1").
		WillReturnRows(timeEntryRows().AddRow(
			"entry_1", "merchant_1", "emp_1", nil, "pointage", now, nil, nil, nil, nil, nil, nil, now, now, nil,
		))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, modified_by, modified_at, modification_reason, created_at, updated_at, deleted_at
		FROM planning_time_entries
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`)).
		WithArgs("merchant_1", "entry_1").
		WillReturnRows(timeEntryRows().AddRow(
			"entry_1", "merchant_1", "emp_1", nil, "pointage", now, nil, nil, nil, nil, nil, nil, now, now, nil,
		))

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE planning_time_entries
		SET clock_out_at = ?, clock_out_note = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1 AND clock_out_at IS NULL
	`)).
		WithArgs(sqlmock.AnyArg(), nil, sqlmock.AnyArg(), "merchant_1", "entry_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest(http.MethodPost, "/planning/me/time-entries/stop", bytes.NewBufferString(`{}`))
	req = req.WithContext(middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1", MerchantRightsID: "42"}))
	rec := httptest.NewRecorder()

	handler.StopCurrentUserTimeEntry(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("StopCurrentUserTimeEntry() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"status":"success"`) {
		t.Fatalf("StopCurrentUserTimeEntry() body = %s, want success status", rec.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestHandlerGetCurrentUserTimeEntrySuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(
		repo,
		stubEmployeeReader{employee: &employeespkg.Employee{ID: "emp_1"}, memberEmployeeID: "emp_1"},
		nil,
		nil,
		nil,
	)
	handler := NewHandler(svc)
	now := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, modified_by, modified_at, modification_reason, created_at, updated_at, deleted_at
		FROM planning_time_entries
		WHERE merchant_id = ? AND employee_id = ? AND clock_out_at IS NULL AND enabled = 1
		ORDER BY clock_in_at DESC, created_at DESC
		LIMIT 1
	`)).
		WithArgs("merchant_1", "emp_1").
		WillReturnRows(timeEntryRows().AddRow(
			"entry_1", "merchant_1", "emp_1", nil, "pointage", now, nil, nil, nil, nil, nil, nil, now, now, nil,
		))

	req := httptest.NewRequest(http.MethodGet, "/planning/me/time-entries/current", nil)
	req = req.WithContext(middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1", MerchantRightsID: "42"}))
	rec := httptest.NewRecorder()

	handler.GetCurrentUserTimeEntry(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GetCurrentUserTimeEntry() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"status":"success"`) {
		t.Fatalf("GetCurrentUserTimeEntry() body = %s, want success status", rec.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestHandlerListCurrentUserTimeEntriesSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(
		repo,
		stubEmployeeReader{employee: &employeespkg.Employee{ID: "emp_1"}, memberEmployeeID: "emp_1"},
		nil,
		nil,
		nil,
	)
	handler := NewHandler(svc)

	fromDate := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	toExclusive := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(1)
		FROM planning_time_entries
		WHERE merchant_id = ? AND employee_id = ? AND clock_in_at >= ? AND clock_in_at < ? AND enabled = 1
	`)).
		WithArgs("merchant_1", "emp_1", fromDate, toExclusive).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, modified_by, modified_at, modification_reason, created_at, updated_at, deleted_at
		FROM planning_time_entries
		WHERE merchant_id = ? AND employee_id = ? AND clock_in_at >= ? AND clock_in_at < ? AND enabled = 1
		ORDER BY clock_in_at DESC, created_at DESC
		LIMIT ? OFFSET ?
	`)).
		WithArgs("merchant_1", "emp_1", fromDate, toExclusive, 20, 0).
		WillReturnRows(timeEntryRows().AddRow(
			"entry_1", "merchant_1", "emp_1", nil, "pointage", now, nil, nil, nil, nil, nil, nil, now, now, nil,
		))

	req := httptest.NewRequest(http.MethodGet, "/planning/me/time-entries?date=2026-06-01", nil)
	req = req.WithContext(middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1", MerchantRightsID: "42"}))
	rec := httptest.NewRecorder()

	handler.ListCurrentUserTimeEntries(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ListCurrentUserTimeEntries() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"status":"success"`) {
		t.Fatalf("ListCurrentUserTimeEntries() body = %s, want success status", rec.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestHandlerCurrentUserTimeEntryRequiresLinkedEmployee(t *testing.T) {
	repo := NewRepository(nil)
	svc := NewService(
		repo,
		stubEmployeeReader{memberErr: sql.ErrNoRows},
		nil,
		nil,
		nil,
	)
	handler := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/planning/me/time-entries/current", nil)
	req = req.WithContext(middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1", MerchantRightsID: "42"}))
	rec := httptest.NewRecorder()

	handler.GetCurrentUserTimeEntry(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GetCurrentUserTimeEntry() status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if !strings.Contains(rec.Body.String(), "planning_employee_not_found") {
		t.Fatalf("GetCurrentUserTimeEntry() body = %s, want planning_employee_not_found", rec.Body.String())
	}
}

func TestHandlerStartCurrentUserTimeEntryRejectsAlreadyOpen(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(
		repo,
		stubEmployeeReader{employee: &employeespkg.Employee{ID: "emp_1"}, memberEmployeeID: "emp_1"},
		nil,
		stubSettingsReader{settings: &settingspkg.PlanningSettings{AttendanceSource: settingspkg.AttendanceSourcePointage}},
		nil,
	)
	handler := NewHandler(svc)
	now := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, modified_by, modified_at, modification_reason, created_at, updated_at, deleted_at
		FROM planning_time_entries
		WHERE merchant_id = ? AND employee_id = ? AND clock_out_at IS NULL AND enabled = 1
		ORDER BY clock_in_at DESC, created_at DESC
		LIMIT 1
	`)).
		WithArgs("merchant_1", "emp_1").
		WillReturnRows(timeEntryRows().AddRow(
			"entry_1", "merchant_1", "emp_1", nil, "pointage", now, nil, nil, nil, nil, nil, nil, now, now, nil,
		))

	req := httptest.NewRequest(http.MethodPost, "/planning/me/time-entries/start", bytes.NewBufferString(`{}`))
	req = req.WithContext(middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1", MerchantRightsID: "42"}))
	rec := httptest.NewRecorder()

	handler.StartCurrentUserTimeEntry(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("StartCurrentUserTimeEntry() status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if !strings.Contains(rec.Body.String(), "planning_time_entry_already_open") {
		t.Fatalf("StartCurrentUserTimeEntry() body = %s, want planning_time_entry_already_open", rec.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
