package schedule

import (
	"context"
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
		ShiftDate:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
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
				SELECT id, merchant_id, label, start_date, end_date, status, notes, created_at, updated_at, deleted_at
				FROM planning_weeks
				WHERE merchant_id = ? AND id = ? AND enabled = 1
				LIMIT 1
			`)).
				WithArgs("merchant_1", "week_1").
				WillReturnRows(sqlmock.NewRows([]string{"id", "merchant_id", "label", "start_date", "end_date", "status", "notes", "created_at", "updated_at", "deleted_at"}).AddRow(
					"week_1", "merchant_1", nil, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC), "draft", nil, now, now, nil,
				))

			mock.ExpectExec(regexp.QuoteMeta(`
				INSERT INTO planning_shifts (
					id, merchant_id, week_id, employee_id, position_id, title, shift_date, start_time, end_time, break_minutes,
					position, location, notes, status, enabled, created_at, updated_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
			`)).
				WithArgs(sqlmock.AnyArg(), "merchant_1", "week_1", nil, nil, "Ouverture", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "09:00:00", "17:00:00", 0, nil, nil, nil, "planned", sqlmock.AnyArg(), sqlmock.AnyArg()).
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
			id, merchant_id, week_id, employee_id, position_id, title, shift_date, start_time, end_time, break_minutes,
			position, location, notes, status, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`)).
		WithArgs(sqlmock.AnyArg(), "merchant_1", "week_1", nil, "pos_1", "Ouverture", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "09:00:00", "17:00:00", 0, "Serveur", nil, nil, "planned", sqlmock.AnyArg(), sqlmock.AnyArg()).
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
					WithArgs("week_1", "emp_1", nil, "Service soir", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "09:00:00", "17:00:00", 0, nil, nil, nil, "planned", sqlmock.AnyArg(), "merchant_1", "shift_1").
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
					WithArgs("week_1", nil, nil, "Ouverture", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "09:00:00", "17:00:00", 0, nil, nil, nil, "planned", sqlmock.AnyArg(), "merchant_1", "shift_1").
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

	err = svc.EnsureShiftHasNoConflicts(context.Background(), "merchant_1", nil, "", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "09:00:00", "17:00:00")
	if err != nil {
		t.Fatalf("EnsureShiftHasNoConflicts() error = %v, want nil", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected SQL expectations: %v", err)
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
		SELECT id, merchant_id, label, start_date, end_date, status, notes, created_at, updated_at, deleted_at
		FROM planning_weeks
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`)).
		WithArgs("merchant_1", "week_1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "merchant_id", "label", "start_date", "end_date", "status", "notes", "created_at", "updated_at", "deleted_at"}).AddRow(
			"week_1", "merchant_1", nil, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC), "draft", nil, now, now, nil,
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
