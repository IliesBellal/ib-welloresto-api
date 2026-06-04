package schedule

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	employeespkg "welloresto-api/internal/modules/planning/employees"
)

type stubEmployeeReader struct {
	employee    *employeespkg.Employee
	employeeErr error
}

func (s stubEmployeeReader) GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*employeespkg.Employee, error) {
	return s.employee, s.employeeErr
}

type stubPositionReader struct {
	positionByID     *employeespkg.EmployeePosition
	positionByIDErr  error
	positionByLabel  *employeespkg.EmployeePosition
	positionByLblErr error
}

func (s stubPositionReader) GetEmployeePositionByID(ctx context.Context, merchantID, positionID string) (*employeespkg.EmployeePosition, error) {
	return s.positionByID, s.positionByIDErr
}

func (s stubPositionReader) GetEmployeePositionByLabel(ctx context.Context, merchantID, label, excludeID string) (*employeespkg.EmployeePosition, error) {
	return s.positionByLabel, s.positionByLblErr
}

func TestPlanningShiftJSONIncludesExplicitNullFields(t *testing.T) {
	shift := PlanningShift{
		ID:           "shift_1",
		MerchantID:   "merchant_1",
		WeekID:       "week_1",
		EmployeeID:   nil,
		PositionID:   nil,
		Title:        "Ouverture",
		ShiftDate:    models.NewDateOnly(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
		StartTime:    "09:00:00",
		EndTime:      "17:00:00",
		BreakMinutes: 30,
		Position:     nil,
		Location:     nil,
		Notes:        nil,
		Status:       "planned",
	}

	payload, err := json.Marshal(shift)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	jsonBody := string(payload)
	if !strings.Contains(jsonBody, `"employee_id":null`) {
		t.Fatalf("json = %s, want explicit employee_id:null", jsonBody)
	}
	if !strings.Contains(jsonBody, `"position_id":null`) {
		t.Fatalf("json = %s, want explicit position_id:null", jsonBody)
	}
	if !strings.Contains(jsonBody, `"position":null`) {
		t.Fatalf("json = %s, want explicit position:null", jsonBody)
	}
	if !strings.Contains(jsonBody, `"location":null`) {
		t.Fatalf("json = %s, want explicit location:null", jsonBody)
	}
	if !strings.Contains(jsonBody, `"notes":null`) {
		t.Fatalf("json = %s, want explicit notes:null", jsonBody)
	}
}

func TestNullableStringPatchFieldUnmarshalTriState(t *testing.T) {
	tests := []struct {
		name      string
		payload   string
		present   bool
		wantValue *string
	}{
		{name: "absent", payload: `{}`, present: false, wantValue: nil},
		{name: "null", payload: `{"employee_id":null}`, present: true, wantValue: nil},
		{name: "value", payload: `{"employee_id":"emp_1"}`, present: true, wantValue: stringPtr("emp_1")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var req PlanningShiftUpdateRequest
			if err := json.Unmarshal([]byte(test.payload), &req); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if req.EmployeeID.Present != test.present {
				t.Fatalf("EmployeeID.Present = %v, want %v", req.EmployeeID.Present, test.present)
			}
			switch {
			case req.EmployeeID.Value == nil && test.wantValue == nil:
				return
			case req.EmployeeID.Value == nil || test.wantValue == nil:
				t.Fatalf("EmployeeID.Value = %#v, want %#v", req.EmployeeID.Value, test.wantValue)
			case *req.EmployeeID.Value != *test.wantValue:
				t.Fatalf("EmployeeID.Value = %q, want %q", *req.EmployeeID.Value, *test.wantValue)
			}
		})
	}
}

func TestServiceCreatePlanningShiftNormalizesUnassignedEmployeeID(t *testing.T) {
	tests := []struct {
		name       string
		employeeID *string
		bodyLabel  string
	}{
		{name: "absent", employeeID: nil, bodyLabel: "missing employee"},
		{name: "empty string", employeeID: stringPtr("   "), bodyLabel: "blank employee"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New() error = %v", err)
			}
			defer db.Close()

			repo := NewRepository(db)
			svc := NewService(repo, stubEmployeeReader{}, stubPositionReader{}, nil)
			ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1"})
			now := time.Now().UTC()

			mock.ExpectQuery(regexp.QuoteMeta(`
				SELECT id, merchant_id, label, start_date, end_date, status, published_at, notes, created_at, updated_at, deleted_at
				FROM planning_weeks
				WHERE merchant_id = ? AND id = ? AND enabled = 1
				LIMIT 1
			`)).
				WithArgs("merchant_1", "week_1").
				WillReturnRows(sqlmock.NewRows([]string{"id", "merchant_id", "label", "start_date", "end_date", "status", "published_at", "notes", "created_at", "updated_at", "deleted_at"}).AddRow(
					"week_1", "merchant_1", nil, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC), "draft", nil, nil, now, now, nil,
				))

			mock.ExpectExec(regexp.QuoteMeta(`
				INSERT INTO planning_shifts (
					id, merchant_id, week_id, employee_id, position_id, shift_date, start_time, end_time, break_minutes,
					position, location, notes, status, enabled, created_at, updated_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
			`)).
				WithArgs(sqlmock.AnyArg(), "merchant_1", "week_1", nil, nil, "2026-06-01", "09:00:00", "17:00:00", 0, nil, nil, nil, "planned", sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnResult(sqlmock.NewResult(0, 1))

			item, err := svc.CreatePlanningShift(ctx, "week_1", PlanningShiftCreateRequest{
				EmployeeID: test.employeeID,
				Title:      "Ouverture",
				ShiftDate:  "2026-06-01",
				StartTime:  "09:00",
				EndTime:    "17:00",
			})
			if err != nil {
				t.Fatalf("CreatePlanningShift() error = %v", err)
			}
			if item.EmployeeID != nil {
				t.Fatalf("CreatePlanningShift() employee_id = %#v, want nil", item.EmployeeID)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet SQL expectations: %v", err)
			}
		})
	}
}

func TestServiceCreatePlanningShiftResolvesPositionID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{}, stubPositionReader{positionByID: &employeespkg.EmployeePosition{ID: "pos_1", Label: "Serveur"}}, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1"})
	now := time.Now().UTC()

	expectWeekLookup(mock, now)
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO planning_shifts (
			id, merchant_id, week_id, employee_id, position_id, shift_date, start_time, end_time, break_minutes,
			position, location, notes, status, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`)).
		WithArgs(sqlmock.AnyArg(), "merchant_1", "week_1", nil, "pos_1", "2026-06-01", "09:00:00", "17:00:00", 0, "Serveur", nil, nil, "planned", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	item, err := svc.CreatePlanningShift(ctx, "week_1", PlanningShiftCreateRequest{
		PositionID: stringPtr("pos_1"),
		Title:      "Ouverture",
		ShiftDate:  "2026-06-01",
		StartTime:  "09:00",
		EndTime:    "17:00",
	})
	if err != nil {
		t.Fatalf("CreatePlanningShift() error = %v", err)
	}
	if item.PositionID == nil || *item.PositionID != "pos_1" {
		t.Fatalf("CreatePlanningShift() position_id = %#v, want pos_1", item.PositionID)
	}
	if item.Position == nil || *item.Position != "Serveur" {
		t.Fatalf("CreatePlanningShift() position = %#v, want Serveur", item.Position)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceUpdatePlanningShiftEmployeeIDTriState(t *testing.T) {
	tests := []struct {
		name         string
		payload      string
		setupSQL     func(mock sqlmock.Sqlmock, now time.Time)
		wantEmployee *string
		wantErr      error
	}{
		{
			name:    "absent keeps assignment",
			payload: `{"title":"Service soir"}`,
			setupSQL: func(mock sqlmock.Sqlmock, now time.Time) {
				expectShiftLookup(mock, now, "emp_1", nil, nil)
				expectWeekLookup(mock, now)
				mock.ExpectQuery(regexp.QuoteMeta(`
					SELECT id, merchant_id, week_id, employee_id, position_id, title, shift_date, start_time, end_time, break_minutes,
						position, location, notes, status, created_at, updated_at, deleted_at
					FROM planning_shifts
					WHERE merchant_id = ? AND employee_id = ? AND shift_date = ? AND enabled = 1
					 AND id <> ? ORDER BY start_time ASC, created_at ASC
				`)).
					WithArgs("merchant_1", "emp_1", "2026-06-01", "shift_1").
					WillReturnRows(shiftRows())
				mock.ExpectExec(regexp.QuoteMeta(`
					UPDATE planning_shifts
					SET week_id = ?, employee_id = ?, position_id = ?, title = ?, shift_date = ?, start_time = ?, end_time = ?, break_minutes = ?,
						position = ?, location = ?, notes = ?, status = ?, updated_at = ?
					WHERE merchant_id = ? AND id = ? AND enabled = 1
				`)).
					WithArgs("week_1", "emp_1", nil, "Service soir", "2026-06-01", "09:00:00", "17:00:00", 0, nil, nil, nil, "planned", sqlmock.AnyArg(), "merchant_1", "shift_1").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantEmployee: stringPtr("emp_1"),
		},
		{
			name:    "null unassigns",
			payload: `{"employee_id":null}`,
			setupSQL: func(mock sqlmock.Sqlmock, now time.Time) {
				expectShiftLookup(mock, now, "emp_1", nil, nil)
				expectWeekLookup(mock, now)
				mock.ExpectExec(regexp.QuoteMeta(`
					UPDATE planning_shifts
					SET week_id = ?, employee_id = ?, position_id = ?, title = ?, shift_date = ?, start_time = ?, end_time = ?, break_minutes = ?,
						position = ?, location = ?, notes = ?, status = ?, updated_at = ?
					WHERE merchant_id = ? AND id = ? AND enabled = 1
				`)).
					WithArgs("week_1", nil, nil, "Ouverture", "2026-06-01", "09:00:00", "17:00:00", 0, nil, nil, nil, "planned", sqlmock.AnyArg(), "merchant_1", "shift_1").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantEmployee: nil,
		},
		{
			name:    "assign new employee with overlap conflict",
			payload: `{"employee_id":"emp_2"}`,
			setupSQL: func(mock sqlmock.Sqlmock, now time.Time) {
				expectShiftLookup(mock, now, "emp_1", nil, nil)
				expectWeekLookup(mock, now)
				mock.ExpectQuery(regexp.QuoteMeta(`
					SELECT id, merchant_id, week_id, employee_id, position_id, title, shift_date, start_time, end_time, break_minutes,
						position, location, notes, status, created_at, updated_at, deleted_at
					FROM planning_shifts
					WHERE merchant_id = ? AND employee_id = ? AND shift_date = ? AND enabled = 1
					 AND id <> ? ORDER BY start_time ASC, created_at ASC
				`)).
					WithArgs("merchant_1", "emp_2", "2026-06-01", "shift_1").
					WillReturnRows(shiftRows().AddRow(
						"shift_2", "merchant_1", "week_1", "emp_2", nil, "Midi", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "12:00:00", "18:00:00", 0, nil, nil, nil, "planned", now, now, nil,
					))
			},
			wantErr: models.ErrPlanningShiftConflict,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New() error = %v", err)
			}
			defer db.Close()

			repo := NewRepository(db)
			svc := NewService(repo, stubEmployeeReader{employee: &employeespkg.Employee{ID: "emp_2"}}, stubPositionReader{}, nil)
			ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1"})
			now := time.Now().UTC()

			test.setupSQL(mock, now)

			var req PlanningShiftUpdateRequest
			if err := json.Unmarshal([]byte(test.payload), &req); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			item, err := svc.UpdatePlanningShift(ctx, "shift_1", req)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("UpdatePlanningShift() error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil {
				switch {
				case item == nil:
					t.Fatal("UpdatePlanningShift() returned nil item")
				case item.EmployeeID == nil && test.wantEmployee == nil:
				case item.EmployeeID == nil || test.wantEmployee == nil:
					t.Fatalf("UpdatePlanningShift() employee_id = %#v, want %#v", item.EmployeeID, test.wantEmployee)
				case *item.EmployeeID != *test.wantEmployee:
					t.Fatalf("UpdatePlanningShift() employee_id = %q, want %q", *item.EmployeeID, *test.wantEmployee)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet SQL expectations: %v", err)
			}
		})
	}
}

func TestEnsureShiftHasNoConflictsSkipsUnassignedShifts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{}, stubPositionReader{}, nil)

	err = svc.EnsureShiftHasNoConflicts(context.Background(), "merchant_1", nil, "", models.NewDateOnly(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)), "09:00:00", "17:00:00")
	if err != nil {
		t.Fatalf("EnsureShiftHasNoConflicts() error = %v, want nil", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected SQL expectations: %v", err)
	}
}

func TestServiceListPlanningShiftsByDateRangeReturnsMultiWeekShifts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{}, stubPositionReader{}, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1"})
	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, week_id, employee_id, position_id, title, shift_date, start_time, end_time, break_minutes,
			position, location, notes, status, created_at, updated_at, deleted_at
		FROM planning_shifts
		WHERE merchant_id = ? AND enabled = 1 AND shift_date >= ? AND shift_date <= ?
		ORDER BY shift_date ASC, start_time ASC, created_at ASC
	`)).
		WithArgs("merchant_1", "2026-06-01", "2026-06-21").
		WillReturnRows(shiftRows().
			AddRow("shift_w1", "merchant_1", "week_1", "emp_1", nil, "Matin", time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), "08:00:00", "12:00:00", 0, nil, nil, nil, "planned", now, now, nil).
			AddRow("shift_w2", "merchant_1", "week_2", nil, nil, "Besoin", time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC), "10:00:00", "14:00:00", 0, nil, nil, nil, "planned", now, now, nil).
			AddRow("shift_w3", "merchant_1", "week_3", "emp_2", "pos_1", "Soir", time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC), "17:00:00", "22:00:00", 0, "Serveur", nil, nil, "planned", now, now, nil))

	items, err := svc.ListPlanningShiftsByDateRange(ctx, "2026-06-01", "2026-06-21")
	if err != nil {
		t.Fatalf("ListPlanningShiftsByDateRange() error = %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("ListPlanningShiftsByDateRange() len = %d, want 3", len(items))
	}
	if items[0].WeekID != "week_1" || items[1].WeekID != "week_2" || items[2].WeekID != "week_3" {
		t.Fatalf("unexpected weeks returned: %#v", items)
	}
	if items[1].EmployeeID != nil {
		t.Fatalf("expected unassigned shift to be included with employee_id=nil, got %#v", items[1].EmployeeID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceListPlanningShiftsByDateRangeRejectsTooWideRange(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{}, stubPositionReader{}, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1"})

	_, err = svc.ListPlanningShiftsByDateRange(ctx, "2026-06-01", "2026-08-05")
	if !errors.Is(err, models.ErrPlanningInvalidDate) {
		t.Fatalf("ListPlanningShiftsByDateRange() error = %v, want %v", err, models.ErrPlanningInvalidDate)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected SQL expectations: %v", err)
	}
}

func TestServiceListPlanningShiftsManagerStillReadsDraftWeek(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{}, stubPositionReader{}, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1"})
	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, label, start_date, end_date, status, published_at, notes, created_at, updated_at, deleted_at
		FROM planning_weeks
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`)).
		WithArgs("merchant_1", "week_1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "merchant_id", "label", "start_date", "end_date", "status", "published_at", "notes", "created_at", "updated_at", "deleted_at"}).AddRow(
			"week_1", "merchant_1", nil, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC), "draft", nil, nil, now, now, nil,
		))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, week_id, employee_id, position_id, title, shift_date, start_time, end_time, break_minutes,
			position, location, notes, status, created_at, updated_at, deleted_at
		FROM planning_shifts
		WHERE merchant_id = ? AND week_id = ? AND enabled = 1
		ORDER BY shift_date ASC, start_time ASC, created_at ASC
	`)).
		WithArgs("merchant_1", "week_1").
		WillReturnRows(shiftRows().AddRow(
			"shift_1", "merchant_1", "week_1", "emp_1", nil, "Ouverture", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "09:00:00", "17:00:00", 0, nil, nil, nil, "planned", now, now, nil,
		))

	items, err := svc.ListPlanningShifts(ctx, "week_1")
	if err != nil {
		t.Fatalf("ListPlanningShifts() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListPlanningShifts() len = %d, want 1", len(items))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServicePublishPlanningWeek(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{}, stubPositionReader{}, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1"})
	now := time.Now().UTC()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE planning_weeks
		SET status = 'published', published_at = COALESCE(published_at, ?), updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`)).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "merchant_1", "week_1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, label, start_date, end_date, status, published_at, notes, created_at, updated_at, deleted_at
		FROM planning_weeks
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`)).
		WithArgs("merchant_1", "week_1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "merchant_id", "label", "start_date", "end_date", "status", "published_at", "notes", "created_at", "updated_at", "deleted_at"}).AddRow(
			"week_1", "merchant_1", nil, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC), "published", now, nil, now, now, nil,
		))

	week, err := svc.PublishPlanningWeek(ctx, "week_1")
	if err != nil {
		t.Fatalf("PublishPlanningWeek() error = %v", err)
	}
	if week.Status != "published" {
		t.Fatalf("PublishPlanningWeek() status = %q, want published", week.Status)
	}
	if week.PublishedAt == nil {
		t.Fatal("PublishPlanningWeek() expected published_at to be set")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceCreatePlanningWeekDefaultsToDraft(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{}, stubPositionReader{}, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1"})

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, label, start_date, end_date, status, published_at, notes, created_at, updated_at, deleted_at
		FROM planning_weeks
		WHERE merchant_id = ? AND start_date = ? AND enabled = 1
		ORDER BY created_at DESC LIMIT 1
	`)).
		WithArgs("merchant_1", "2026-06-01").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO planning_weeks (
			id, merchant_id, label, start_date, end_date, status, published_at, notes, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`)).
		WithArgs(sqlmock.AnyArg(), "merchant_1", nil, sqlmock.AnyArg(), sqlmock.AnyArg(), "draft", nil, nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	week, err := svc.CreatePlanningWeek(ctx, PlanningWeekCreateRequest{StartDate: "2026-06-01", EndDate: "2026-06-07"})
	if err != nil {
		t.Fatalf("CreatePlanningWeek() error = %v", err)
	}
	if week.Status != "draft" {
		t.Fatalf("CreatePlanningWeek() status = %q, want draft", week.Status)
	}
	if week.PublishedAt != nil {
		t.Fatalf("CreatePlanningWeek() published_at = %v, want nil", week.PublishedAt)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceUnpublishPlanningWeek(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{}, stubPositionReader{}, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1"})
	now := time.Now().UTC()

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE planning_weeks
		SET status = 'draft', published_at = NULL, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`)).
		WithArgs(sqlmock.AnyArg(), "merchant_1", "week_1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, label, start_date, end_date, status, published_at, notes, created_at, updated_at, deleted_at
		FROM planning_weeks
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`)).
		WithArgs("merchant_1", "week_1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "merchant_id", "label", "start_date", "end_date", "status", "published_at", "notes", "created_at", "updated_at", "deleted_at"}).AddRow(
			"week_1", "merchant_1", nil, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC), "draft", nil, nil, now, now, nil,
		))

	week, err := svc.UnpublishPlanningWeek(ctx, "week_1")
	if err != nil {
		t.Fatalf("UnpublishPlanningWeek() error = %v", err)
	}
	if week.Status != "draft" {
		t.Fatalf("UnpublishPlanningWeek() status = %q, want draft", week.Status)
	}
	if week.PublishedAt != nil {
		t.Fatalf("UnpublishPlanningWeek() published_at = %v, want nil", week.PublishedAt)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServicePublishPlanningWeekIdempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{}, stubPositionReader{}, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1"})
	publishedAt := time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC)
	now := time.Now().UTC()

	for i := 0; i < 2; i++ {
		mock.ExpectExec(regexp.QuoteMeta(`
			UPDATE planning_weeks
			SET status = 'published', published_at = COALESCE(published_at, ?), updated_at = ?
			WHERE merchant_id = ? AND id = ? AND enabled = 1
		`)).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "merchant_1", "week_1").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(regexp.QuoteMeta(`
			SELECT id, merchant_id, label, start_date, end_date, status, published_at, notes, created_at, updated_at, deleted_at
			FROM planning_weeks
			WHERE merchant_id = ? AND id = ? AND enabled = 1
			LIMIT 1
		`)).
			WithArgs("merchant_1", "week_1").
			WillReturnRows(sqlmock.NewRows([]string{"id", "merchant_id", "label", "start_date", "end_date", "status", "published_at", "notes", "created_at", "updated_at", "deleted_at"}).AddRow(
				"week_1", "merchant_1", nil, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC), "published", publishedAt, nil, now, now, nil,
			))

		week, callErr := svc.PublishPlanningWeek(ctx, "week_1")
		if callErr != nil {
			t.Fatalf("PublishPlanningWeek() call %d error = %v", i+1, callErr)
		}
		if week.Status != "published" || week.PublishedAt == nil {
			t.Fatalf("PublishPlanningWeek() call %d returned unexpected week: %#v", i+1, week)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceUpdatePlanningShiftInPublishedWeekStillAllowed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{}, stubPositionReader{}, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1"})
	now := time.Now().UTC()

	expectShiftLookup(mock, now, "emp_1", nil, nil)
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, label, start_date, end_date, status, published_at, notes, created_at, updated_at, deleted_at
		FROM planning_weeks
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`)).
		WithArgs("merchant_1", "week_1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "merchant_id", "label", "start_date", "end_date", "status", "published_at", "notes", "created_at", "updated_at", "deleted_at"}).AddRow(
			"week_1", "merchant_1", nil, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC), "published", now, nil, now, now, nil,
		))
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, week_id, employee_id, position_id, title, shift_date, start_time, end_time, break_minutes,
			position, location, notes, status, created_at, updated_at, deleted_at
		FROM planning_shifts
		WHERE merchant_id = ? AND employee_id = ? AND shift_date = ? AND enabled = 1
		 AND id <> ? ORDER BY start_time ASC, created_at ASC
	`)).
		WithArgs("merchant_1", "emp_1", "2026-06-01", "shift_1").
		WillReturnRows(shiftRows())
	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE planning_shifts
		SET week_id = ?, employee_id = ?, position_id = ?, title = ?, shift_date = ?, start_time = ?, end_time = ?, break_minutes = ?,
			position = ?, location = ?, notes = ?, status = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`)).
		WithArgs("week_1", "emp_1", nil, "Service soir", "2026-06-01", "09:00:00", "17:00:00", 0, nil, nil, nil, "planned", sqlmock.AnyArg(), "merchant_1", "shift_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	item, err := svc.UpdatePlanningShift(ctx, "shift_1", PlanningShiftUpdateRequest{Title: stringPtr("Service soir")})
	if err != nil {
		t.Fatalf("UpdatePlanningShift() error = %v", err)
	}
	if item == nil || item.Title != "Service soir" {
		t.Fatalf("UpdatePlanningShift() returned unexpected item: %#v", item)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func expectShiftLookup(mock sqlmock.Sqlmock, now time.Time, employeeID any, positionID any, position any) {
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, week_id, employee_id, position_id, title, shift_date, start_time, end_time, break_minutes,
			position, location, notes, status, created_at, updated_at, deleted_at
		FROM planning_shifts
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`)).
		WithArgs("merchant_1", "shift_1").
		WillReturnRows(shiftRows().AddRow(
			"shift_1", "merchant_1", "week_1", employeeID, positionID, "Ouverture", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "09:00:00", "17:00:00", 0, position, nil, nil, "planned", now, now, nil,
		))
}

func expectWeekLookup(mock sqlmock.Sqlmock, now time.Time) {
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, label, start_date, end_date, status, published_at, notes, created_at, updated_at, deleted_at
		FROM planning_weeks
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`)).
		WithArgs("merchant_1", "week_1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "merchant_id", "label", "start_date", "end_date", "status", "published_at", "notes", "created_at", "updated_at", "deleted_at"}).AddRow(
			"week_1", "merchant_1", nil, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC), "draft", nil, nil, now, now, nil,
		))
}

func shiftRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "merchant_id", "week_id", "employee_id", "position_id", "title", "shift_date", "start_time", "end_time", "break_minutes",
		"position", "location", "notes", "status", "created_at", "updated_at", "deleted_at",
	})
}

func employeeRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "merchant_id", "user_id", "member_id", "first_name", "last_name", "position_id", "position", "position_note", "job_title", "email", "phone", "role",
		"contract_type_code", "contract_start_date", "contract_end_date", "probation_end_date", "last_medical_checkup_date",
		"contract_hours", "max_weekly_hours", "required_rest_days", "sunday_premium", "night_premium",
		"hourly_rate", "gross_monthly_salary", "employer_charges_pct", "transport_cost", "birth_date", "gender", "nationality",
		"address", "hr_comment", "active", "created_at", "updated_at", "deleted_at",
	})
}

func stringPtr(value string) *string {
	return &value
}
