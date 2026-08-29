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
	daycommentspkg "welloresto-api/internal/modules/planning/daycomments"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	schedulepkg "welloresto-api/internal/modules/planning/schedule"
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
		nil,
	)
	handler := NewHandler(svc)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, modified_by, modified_at, modification_reason, created_at, updated_at, deleted_at
		FROM planning_time_entries
		WHERE merchant_id = ? AND employee_id = ? AND clock_out_at IS NULL AND enabled = TRUE
		ORDER BY clock_in_at DESC, created_at DESC
		LIMIT 1
	`)).
		WithArgs("merchant_1", "emp_1").
		WillReturnError(sql.ErrNoRows)

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO planning_time_entries (
			id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, modified_by, modified_at, modification_reason, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, TRUE, ?, ?)
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
		nil,
	)
	handler := NewHandler(svc)
	now := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, modified_by, modified_at, modification_reason, created_at, updated_at, deleted_at
		FROM planning_time_entries
		WHERE merchant_id = ? AND employee_id = ? AND clock_out_at IS NULL AND enabled = TRUE
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
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE
		LIMIT 1
	`)).
		WithArgs("merchant_1", "entry_1").
		WillReturnRows(timeEntryRows().AddRow(
			"entry_1", "merchant_1", "emp_1", nil, "pointage", now, nil, nil, nil, nil, nil, nil, now, now, nil,
		))

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE planning_time_entries
		SET clock_out_at = ?, clock_out_note = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = TRUE AND clock_out_at IS NULL
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
		nil,
	)
	handler := NewHandler(svc)
	now := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, modified_by, modified_at, modification_reason, created_at, updated_at, deleted_at
		FROM planning_time_entries
		WHERE merchant_id = ? AND employee_id = ? AND clock_out_at IS NULL AND enabled = TRUE
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
		nil,
	)
	handler := NewHandler(svc)

	fromDate := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	toExclusive := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(1)
		FROM planning_time_entries
		WHERE merchant_id = ? AND employee_id = ? AND clock_in_at >= ? AND clock_in_at < ? AND enabled = TRUE
	`)).
		WithArgs("merchant_1", "emp_1", fromDate, toExclusive).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, modified_by, modified_at, modification_reason, created_at, updated_at, deleted_at
		FROM planning_time_entries
		WHERE merchant_id = ? AND employee_id = ? AND clock_in_at >= ? AND clock_in_at < ? AND enabled = TRUE
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
		nil,
	)
	handler := NewHandler(svc)
	now := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, modified_by, modified_at, modification_reason, created_at, updated_at, deleted_at
		FROM planning_time_entries
		WHERE merchant_id = ? AND employee_id = ? AND clock_out_at IS NULL AND enabled = TRUE
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

func TestHandlerListCurrentUserTeamWeekShiftsIncludesPositionColorAndEmployeeName(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	shiftRepo := schedulepkg.NewRepository(db)
	svc := NewService(
		repo,
		stubEmployeeReader{employee: &employeespkg.Employee{ID: "emp_1"}, memberEmployeeID: "emp_1"},
		shiftRepo,
		nil,
		nil,
		stubDayCommentReader{
			comments: []daycommentspkg.PlanningDayComment{
				{ID: "plan-day-comment-1", MerchantID: "merchant_1", Comment: "Jour ferie, horaires speciaux"},
			},
		},
	)
	handler := NewHandler(svc)
	now := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, label, start_date, end_date, status, published_at, notes, created_at, updated_at, deleted_at
		FROM planning_weeks
		WHERE merchant_id = ? AND start_date = ? AND enabled = TRUE
		 ORDER BY created_at DESC LIMIT 1
	`)).
		WithArgs("merchant_1", "2026-06-01").
		WillReturnRows(sqlmock.NewRows([]string{"id", "merchant_id", "label", "start_date", "end_date", "status", "published_at", "notes", "created_at", "updated_at", "deleted_at"}).AddRow(
			"week_1", "merchant_1", nil, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC), "published", now, nil, now, now, nil,
		))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT s.id, s.merchant_id, s.week_id, s.employee_id,
			NULLIF(TRIM(CONCAT(COALESCE(e.first_name, ''), ' ', COALESCE(e.last_name, ''))), '') AS employee_name,
			s.position_id, s.shift_date, s.start_time, s.end_time, s.break_minutes,
			s.position, p.color, s.notes, s.status, s.created_at, s.updated_at, s.deleted_at
		FROM planning_shifts s
		LEFT JOIN employees e ON e.id = s.employee_id AND e.merchant_id = s.merchant_id AND e.enabled = TRUE
		LEFT JOIN planning_positions p ON p.id = s.position_id AND p.merchant_id = s.merchant_id AND p.enabled = TRUE
		WHERE s.merchant_id = ? AND s.week_id = ? AND s.enabled = TRUE AND s.status = 'published'
		ORDER BY s.shift_date ASC, s.start_time ASC, s.created_at ASC
	`)).
		WithArgs("merchant_1", "week_1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "merchant_id", "week_id", "employee_id", "employee_name", "position_id", "shift_date", "start_time", "end_time", "break_minutes",
			"position", "color", "notes", "status", "created_at", "updated_at", "deleted_at",
		}).AddRow(
			"shift_1", "merchant_1", "week_1", "emp_2", "Alice Martin", "pos_1", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "10:00:00", "14:00:00", 30,
			"Serveuse", "#3b82f6", "Consignes", "published", now, now, nil,
		))

	req := httptest.NewRequest(http.MethodGet, "/planning/me/team-week?week_start=2026-06-01", nil)
	req = req.WithContext(middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1", MerchantRightsID: "42"}))
	rec := httptest.NewRecorder()

	handler.ListCurrentUserTeamWeekShifts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ListCurrentUserTeamWeekShifts() status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"position_color":"#3b82f6"`) {
		t.Fatalf("ListCurrentUserTeamWeekShifts() body = %s, want position_color", body)
	}
	if !strings.Contains(body, `"employee_name":"Alice Martin"`) {
		t.Fatalf("ListCurrentUserTeamWeekShifts() body = %s, want employee_name", body)
	}
	if !strings.Contains(body, `"day_comments":[{"id":"plan-day-comment-1"`) || !strings.Contains(body, `Jour ferie, horaires speciaux`) {
		t.Fatalf("ListCurrentUserTeamWeekShifts() body = %s, want day_comments with the seeded comment", body)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
