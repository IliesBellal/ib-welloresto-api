package weektemplates

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	authpkg "welloresto-api/internal/modules/auth"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	leavepkg "welloresto-api/internal/modules/planning/leave"
	schedulepkg "welloresto-api/internal/modules/planning/schedule"

	"github.com/DATA-DOG/go-sqlmock"
)

type employeeReaderStub struct {
	employees map[string]bool
	positions map[string]bool
}

func (s *employeeReaderStub) GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*employeespkg.Employee, error) {
	if s.employees[employeeID] {
		return &employeespkg.Employee{ID: employeeID, MerchantID: merchantID}, nil
	}
	return nil, sql.ErrNoRows
}

func (s *employeeReaderStub) GetEmployeePositionByID(ctx context.Context, merchantID, id string) (*employeespkg.EmployeePosition, error) {
	if s.positions[id] {
		return &employeespkg.EmployeePosition{ID: id, MerchantID: merchantID}, nil
	}
	return nil, sql.ErrNoRows
}

func (s *employeeReaderStub) GetEmployeePositionByLabel(ctx context.Context, merchantID, label, excludeID string) (*employeespkg.EmployeePosition, error) {
	if s.positions[label] {
		return &employeespkg.EmployeePosition{ID: s.mustPositionID(label), MerchantID: merchantID, Active: true}, nil
	}
	return nil, sql.ErrNoRows
}

func (s *employeeReaderStub) mustPositionID(label string) string {
	if strings.HasPrefix(label, "pos-") {
		return label
	}
	return "pos-" + strings.ReplaceAll(strings.ToLower(label), " ", "-")
}

type weekSourceReaderStub struct {
	week            *schedulepkg.PlanningWeek
	weeksByStart    map[string]*schedulepkg.PlanningWeek
	shifts          []schedulepkg.PlanningShift
	err             error
	createWeekErr   error
	createShiftErr  error
	deleteShiftErr  error
	createdWeeks    []schedulepkg.PlanningWeek
	createdShifts   []schedulepkg.PlanningShift
	deletedShiftIDs []string
}

func (s *weekSourceReaderStub) GetPlanningWeekByID(ctx context.Context, merchantID, weekID string) (*schedulepkg.PlanningWeek, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.week == nil {
		return nil, sql.ErrNoRows
	}
	return s.week, nil
}

func (s *weekSourceReaderStub) ListPlanningShifts(ctx context.Context, merchantID, weekID string) ([]schedulepkg.PlanningShift, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.shifts, nil
}

func (s *weekSourceReaderStub) GetPlanningWeekByStartDate(ctx context.Context, merchantID string, startDate time.Time, excludeWeekID string) (*schedulepkg.PlanningWeek, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.weeksByStart == nil {
		return nil, sql.ErrNoRows
	}
	item, ok := s.weeksByStart[startDate.Format("2006-01-02")]
	if !ok || item == nil {
		return nil, sql.ErrNoRows
	}
	return item, nil
}

func (s *weekSourceReaderStub) CreatePlanningWeek(ctx context.Context, merchantID string, week schedulepkg.PlanningWeek) (*schedulepkg.PlanningWeek, error) {
	if s.createWeekErr != nil {
		return nil, s.createWeekErr
	}
	if strings.TrimSpace(week.ID) == "" {
		week.ID = "wk-created"
	}
	created := week
	if s.weeksByStart == nil {
		s.weeksByStart = map[string]*schedulepkg.PlanningWeek{}
	}
	key := week.StartDate.Format("2006-01-02")
	s.weeksByStart[key] = &created
	s.createdWeeks = append(s.createdWeeks, created)
	return &created, nil
}

func (s *weekSourceReaderStub) CreatePlanningShift(ctx context.Context, merchantID string, shift schedulepkg.PlanningShift) (*schedulepkg.PlanningShift, error) {
	if s.createShiftErr != nil {
		return nil, s.createShiftErr
	}
	if strings.TrimSpace(shift.ID) == "" {
		shift.ID = "sh-created-" + shift.ShiftDate.Time().Format("20060102") + "-" + shift.StartTime
	}
	created := shift
	s.createdShifts = append(s.createdShifts, created)
	return &created, nil
}

func (s *weekSourceReaderStub) SoftDeletePlanningShift(ctx context.Context, merchantID, shiftID string) error {
	if s.deleteShiftErr != nil {
		return s.deleteShiftErr
	}
	s.deletedShiftIDs = append(s.deletedShiftIDs, shiftID)
	return nil
}

type leaveReaderStub struct {
	leaves []leavepkg.PlanningLeaveRequest
	err    error
}

func (s *leaveReaderStub) ListApprovedLeavesOverlappingRange(ctx context.Context, merchantID string, employeeIDs []string, startDate, endDate time.Time) ([]leavepkg.PlanningLeaveRequest, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.leaves, nil
}

func withPlanningContext() context.Context {
	user := &authpkg.UserLoginRow{MerchantID: "m-1", UserID: "u-1"}
	return middleware.WithUser(context.Background(), user)
}

func TestCreateWeekTemplateRequiresShiftsField(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}
	defer db.Close()

	svc := NewService(NewRepository(db), &employeeReaderStub{}, nil, nil, nil)
	_, err = svc.CreateWeekTemplate(withPlanningContext(), WeekTemplateCreateRequest{Label: "S1"})
	if err != models.ErrInvalidData {
		t.Fatalf("expected ErrInvalidData, got %v", err)
	}
}

func TestCreateWeekTemplateAcceptsEmptyShifts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}
	defer db.Close()

	svc := NewService(NewRepository(db), &employeeReaderStub{}, nil, nil, nil)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO planning_week_templates").
		WithArgs(sqlmock.AnyArg(), "m-1", "Template A", nil, true).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	res, err := svc.CreateWeekTemplate(withPlanningContext(), WeekTemplateCreateRequest{
		Label:  "Template A",
		Shifts: WeekTemplateShiftInputsField{Present: true, Value: []WeekTemplateShiftInput{}},
	})
	if err != nil {
		t.Fatalf("create week template: %v", err)
	}
	if res.ShiftCount != 0 {
		t.Fatalf("expected shift_count 0, got %d", res.ShiftCount)
	}
	if res.MerchantID != "m-1" {
		t.Fatalf("expected merchant_id m-1, got %s", res.MerchantID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestCreateWeekTemplateAllowsNullEmployeeID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}
	defer db.Close()

	svc := NewService(NewRepository(db), &employeeReaderStub{positions: map[string]bool{"pos-1": true}}, nil, nil, nil)
	day := 1
	start := "09:00"
	end := "17:00"
	breakMins := 30
	positionID := "pos-1"

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO planning_week_templates").
		WithArgs(sqlmock.AnyArg(), "m-1", "Template B", nil, true).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO planning_week_template_shifts").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), day, nil, positionID, nil, "09:00", "17:00", breakMins, nil, nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	_, err = svc.CreateWeekTemplate(withPlanningContext(), WeekTemplateCreateRequest{
		Label: "Template B",
		Shifts: WeekTemplateShiftInputsField{Present: true, Value: []WeekTemplateShiftInput{
			{
				DayOfWeek:    &day,
				EmployeeID:   nil,
				PositionID:   &positionID,
				StartTime:    &start,
				EndTime:      &end,
				BreakMinutes: &breakMins,
			},
		}},
	})
	if err != nil {
		t.Fatalf("create week template: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestUpdateWeekTemplateWithShiftsReplacesAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, &employeeReaderStub{}, nil, nil, nil)

	listCols := []string{"id", "merchant_id", "label", "notes", "active", "shift_count", "created_at", "updated_at"}
	createdAt := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2025, 1, 1, 10, 5, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT wt.id, wt.merchant_id").
		WithArgs("m-1", "wt-1").
		WillReturnRows(sqlmock.NewRows(listCols).AddRow("wt-1", "m-1", "Old", nil, true, 1, createdAt, createdAt))

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE planning_week_templates").
		WithArgs("New", nil, true, "m-1", "wt-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("DELETE FROM planning_week_template_shifts").
		WithArgs("wt-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO planning_week_template_shifts").
		WithArgs(sqlmock.AnyArg(), "wt-1", 2, nil, nil, nil, "10:00", "18:00", 0, nil, nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	mock.ExpectQuery("SELECT wt.id, wt.merchant_id").
		WithArgs("m-1", "wt-1").
		WillReturnRows(sqlmock.NewRows(listCols).AddRow("wt-1", "m-1", "New", nil, true, 1, createdAt, updatedAt))
	shiftCols := []string{"id", "week_template_id", "day_of_week", "employee_id", "position_id", "title", "start_time", "end_time", "break_minutes", "location", "notes", "created_at", "updated_at"}
	mock.ExpectQuery("SELECT s.id,").
		WithArgs("m-1", "wt-1").
		WillReturnRows(sqlmock.NewRows(shiftCols).AddRow("s-1", "wt-1", 2, nil, nil, nil, "10:00", "18:00", 0, nil, nil, updatedAt, updatedAt))

	label := "New"
	day := 2
	start := "10:00"
	end := "18:00"
	_, err = svc.UpdateWeekTemplate(withPlanningContext(), "wt-1", WeekTemplateUpdateRequest{
		Label: &label,
		Shifts: WeekTemplateShiftInputsField{Present: true, Value: []WeekTemplateShiftInput{
			{DayOfWeek: &day, StartTime: &start, EndTime: &end},
		}},
	})
	if err != nil {
		t.Fatalf("update week template: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestWeekTemplateShiftMarshalKeepsNulls(t *testing.T) {
	var employeeID *string
	var positionID *string
	shift := WeekTemplateShift{EmployeeID: employeeID, PositionID: positionID}
	payload, err := json.Marshal(shift)
	if err != nil {
		t.Fatalf("marshal shift: %v", err)
	}
	if string(payload) == "" {
		t.Fatal("expected non-empty json")
	}
	if !strings.Contains(string(payload), `"employee_id":null`) {
		t.Fatalf("expected employee_id null in json: %s", string(payload))
	}
	if !strings.Contains(string(payload), `"position_id":null`) {
		t.Fatalf("expected position_id null in json: %s", string(payload))
	}
}

func TestCreateWeekTemplateFromWeekMapsDayAndPreservesEmployees(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}
	defer db.Close()

	weekSource := &weekSourceReaderStub{
		week: &schedulepkg.PlanningWeek{ID: "w-current", MerchantID: "m-1"},
		shifts: []schedulepkg.PlanningShift{
			{
				ShiftDate:    models.NewDateOnly(time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC)),
				EmployeeID:   strPtr("emp-1"),
				PositionID:   strPtr("pos-1"),
				Title:        "",
				StartTime:    "09:00:00",
				EndTime:      "17:00:00",
				BreakMinutes: 30,
				Location:     strPtr("Salle"),
				Notes:        nil,
			},
			{
				ShiftDate:    models.NewDateOnly(time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)),
				EmployeeID:   nil,
				PositionID:   nil,
				Position:     strPtr("bar"),
				Title:        "Ouverture",
				StartTime:    "10:00",
				EndTime:      "18:00",
				BreakMinutes: 0,
				Location:     nil,
				Notes:        strPtr("Briefing"),
			},
		},
	}
	svc := NewService(NewRepository(db), &employeeReaderStub{positions: map[string]bool{"bar": true}}, weekSource, nil, nil)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO planning_week_templates").
		WithArgs(sqlmock.AnyArg(), "m-1", "Modele semaine", "Forte affluence", true).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO planning_week_template_shifts").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 0, "emp-1", "pos-1", nil, "09:00", "17:00", 30, "Salle", nil).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO planning_week_template_shifts").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 1, nil, "pos-bar", "Ouverture", "10:00", "18:00", 0, nil, "Briefing").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	res, err := svc.CreateWeekTemplateFromWeek(withPlanningContext(), WeekTemplateFromWeekRequest{
		WeekID: "w-current",
		Label:  "Modele semaine",
		Notes:  strPtr("Forte affluence"),
	})
	if err != nil {
		t.Fatalf("create from week: %v", err)
	}
	if len(res.WeekTemplateShifts) != 2 {
		t.Fatalf("expected 2 shifts, got %d", len(res.WeekTemplateShifts))
	}
	if res.WeekTemplateShifts[0].DayOfWeek != 0 || res.WeekTemplateShifts[1].DayOfWeek != 1 {
		t.Fatalf("expected sunday=0 and monday=1, got %d and %d", res.WeekTemplateShifts[0].DayOfWeek, res.WeekTemplateShifts[1].DayOfWeek)
	}
	if res.WeekTemplateShifts[0].EmployeeID == nil || *res.WeekTemplateShifts[0].EmployeeID != "emp-1" {
		t.Fatalf("expected nominative employee_id preserved")
	}
	if res.WeekTemplateShifts[1].EmployeeID != nil {
		t.Fatalf("expected null employee_id preserved")
	}
	if res.WeekTemplateShifts[0].Title != nil {
		t.Fatalf("expected empty title to map to nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestCreateWeekTemplateFromWeekAcceptsWeekWithoutShifts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}
	defer db.Close()

	weekSource := &weekSourceReaderStub{week: &schedulepkg.PlanningWeek{ID: "w-empty", MerchantID: "m-1"}, shifts: []schedulepkg.PlanningShift{}}
	svc := NewService(NewRepository(db), &employeeReaderStub{}, weekSource, nil, nil)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO planning_week_templates").
		WithArgs(sqlmock.AnyArg(), "m-1", "Semaine vide", nil, true).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	res, err := svc.CreateWeekTemplateFromWeek(withPlanningContext(), WeekTemplateFromWeekRequest{WeekID: "w-empty", Label: "Semaine vide"})
	if err != nil {
		t.Fatalf("create from week: %v", err)
	}
	if len(res.WeekTemplateShifts) != 0 {
		t.Fatalf("expected 0 shifts, got %d", len(res.WeekTemplateShifts))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func strPtr(v string) *string { return &v }

func TestPreviewWeekTemplateInstantiationDistinctAndAutoUnassigned(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}
	defer db.Close()

	weekStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	weekSource := &weekSourceReaderStub{
		weeksByStart: map[string]*schedulepkg.PlanningWeek{
			"2026-06-01": {ID: "wk-1", MerchantID: "m-1", StartDate: weekStart},
		},
		shifts: []schedulepkg.PlanningShift{
			{ID: "sh-overlap", EmployeeID: strPtr("emp-1"), ShiftDate: models.NewDateOnly(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)), StartTime: "09:30:00", EndTime: "12:00:00"},
		},
	}
	leaves := []leavepkg.PlanningLeaveRequest{
		{EmployeeID: "emp-1", Status: "approved", StartDate: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)},
	}
	svc := NewService(NewRepository(db), &employeeReaderStub{employees: map[string]bool{"emp-1": true}}, weekSource, &leaveReaderStub{leaves: leaves}, nil)

	tplCols := []string{"id", "merchant_id", "label", "notes", "active", "shift_count", "created_at", "updated_at"}
	shiftCols := []string{"id", "week_template_id", "day_of_week", "employee_id", "position_id", "title", "start_time", "end_time", "break_minutes", "location", "notes", "created_at", "updated_at"}
	createdAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT wt.id, wt.merchant_id").
		WithArgs("m-1", "wt-1").
		WillReturnRows(sqlmock.NewRows(tplCols).AddRow("wt-1", "m-1", "Tpl", nil, true, 3, createdAt, createdAt))
	mock.ExpectQuery("SELECT s.id,").
		WithArgs("m-1", "wt-1").
		WillReturnRows(sqlmock.NewRows(shiftCols).
			AddRow("t1", "wt-1", 1, "emp-1", nil, "A", "09:00", "11:00", 0, nil, nil, createdAt, createdAt).
			AddRow("t2", "wt-1", 2, "emp-1", nil, "B", "09:00", "11:00", 0, nil, nil, createdAt, createdAt).
			AddRow("t3", "wt-1", 3, nil, nil, nil, "09:00", "11:00", 0, nil, nil, createdAt, createdAt))

	preview, err := svc.PreviewWeekTemplateInstantiation(withPlanningContext(), "wt-1", WeekTemplatePreviewRequest{TargetWeekStarts: []string{"2026-06-01"}})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.ImpactedEmployeeCount != 1 {
		t.Fatalf("expected impacted_employee_count=1, got %d", preview.ImpactedEmployeeCount)
	}
	if preview.AutoUnassignedCount != 1 {
		t.Fatalf("expected auto_unassigned_count=1, got %d", preview.AutoUnassignedCount)
	}
	if preview.IdempotentSkippedCount != 0 {
		t.Fatalf("expected idempotent_skipped_count=0, got %d", preview.IdempotentSkippedCount)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func expectInstantiateTemplateQueries(mock sqlmock.Sqlmock, templateID string, rows []WeekTemplateShift) {
	tplCols := []string{"id", "merchant_id", "label", "notes", "active", "shift_count", "created_at", "updated_at"}
	shiftCols := []string{"id", "week_template_id", "day_of_week", "employee_id", "position_id", "title", "start_time", "end_time", "break_minutes", "location", "notes", "created_at", "updated_at"}
	createdAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery("SELECT wt.id, wt.merchant_id").
		WithArgs("m-1", templateID).
		WillReturnRows(sqlmock.NewRows(tplCols).AddRow(templateID, "m-1", "Tpl", nil, true, len(rows), createdAt, createdAt))

	shiftRows := sqlmock.NewRows(shiftCols)
	for _, item := range rows {
		shiftRows.AddRow(item.ID, templateID, item.DayOfWeek, item.EmployeeID, item.PositionID, item.Title, item.StartTime, item.EndTime, item.BreakMinutes, item.Location, item.Notes, createdAt, createdAt)
	}
	mock.ExpectQuery("SELECT s.id,").
		WithArgs("m-1", templateID).
		WillReturnRows(shiftRows)
}

func TestInstantiateWeekTemplate_OverlapModes(t *testing.T) {
	modes := []ConflictMode{ConflictModeKeepExisting, ConflictModeReplace, ConflictModeTemplateToUnassigned}
	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock new: %v", err)
			}
			defer db.Close()

			empID := "emp-1"
			weekStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
			weekSource := &weekSourceReaderStub{
				weeksByStart: map[string]*schedulepkg.PlanningWeek{"2026-06-01": {ID: "wk-1", MerchantID: "m-1", StartDate: weekStart}},
				shifts:       []schedulepkg.PlanningShift{{ID: "sh-overlap", EmployeeID: &empID, ShiftDate: models.NewDateOnly(weekStart), StartTime: "09:30:00", EndTime: "10:30:00"}},
			}
			svc := NewService(NewRepository(db), &employeeReaderStub{employees: map[string]bool{"emp-1": true}}, weekSource, &leaveReaderStub{}, nil)

			expectInstantiateTemplateQueries(mock, "wt-1", []WeekTemplateShift{{ID: "ts-1", DayOfWeek: 1, EmployeeID: &empID, StartTime: "09:00", EndTime: "11:00"}})
			mock.ExpectBegin()
			if mode == ConflictModeReplace {
				mock.ExpectCommit()
			} else {
				mock.ExpectCommit()
			}

			result, err := svc.InstantiateWeekTemplate(withPlanningContext(), "wt-1", WeekTemplateInstantiateRequest{TargetWeekStarts: []string{"2026-06-01"}, ConflictMode: mode})
			if err != nil {
				t.Fatalf("instantiate: %v", err)
			}

			switch mode {
			case ConflictModeKeepExisting:
				if result.CreatedCount != 0 || result.SkippedCount != 1 || result.ReplacedCount != 0 {
					t.Fatalf("unexpected counters for keep_existing: %+v", *result)
				}
			case ConflictModeReplace:
				if result.CreatedCount != 1 || result.AssignedCount != 1 || result.ReplacedCount != 1 || result.SkippedCount != 0 {
					t.Fatalf("unexpected counters for replace: %+v", *result)
				}
				if len(weekSource.deletedShiftIDs) != 1 || weekSource.deletedShiftIDs[0] != "sh-overlap" {
					t.Fatalf("replace should delete overlap shift first")
				}
			case ConflictModeTemplateToUnassigned:
				if result.CreatedCount != 1 || result.UnassignedCount != 1 || result.AssignedCount != 0 {
					t.Fatalf("unexpected counters for template_to_unassigned: %+v", *result)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("expectations: %v", err)
			}
		})
	}
}

func TestInstantiateWeekTemplate_SafetyNetsAndNeedAreUnassigned(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}
	defer db.Close()

	weekStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	contractEnd := weekStart.AddDate(0, 0, -1)
	weekSource := &weekSourceReaderStub{weeksByStart: map[string]*schedulepkg.PlanningWeek{"2026-06-01": {ID: "wk-1", MerchantID: "m-1", StartDate: weekStart}}}
	leaveReader := &leaveReaderStub{leaves: []leavepkg.PlanningLeaveRequest{{EmployeeID: "emp-leave", Status: "approved", StartDate: weekStart, EndDate: weekStart}}}
	employeeRepo := &employeeReaderStub{employees: map[string]bool{"emp-leave": true, "emp-ended": true}}
	svc := NewService(NewRepository(db), employeeRepo, weekSource, leaveReader, nil)

	expectInstantiateTemplateQueries(mock, "wt-2", []WeekTemplateShift{
		{ID: "need", DayOfWeek: 1, EmployeeID: nil, StartTime: "08:00", EndTime: "10:00"},
		{ID: "leave", DayOfWeek: 1, EmployeeID: strPtr("emp-leave"), StartTime: "10:00", EndTime: "12:00"},
		{ID: "ended", DayOfWeek: 1, EmployeeID: strPtr("emp-ended"), StartTime: "12:00", EndTime: "14:00"},
	})
	mock.ExpectBegin()
	mock.ExpectCommit()

	// enrich employee contract end for emp-ended using override after repository check
	svc.employeeRepo = &employeeReaderStubWithContractEnd{
		base:        employeeRepo,
		contractEnd: map[string]time.Time{"emp-ended": contractEnd},
	}

	result, err := svc.InstantiateWeekTemplate(withPlanningContext(), "wt-2", WeekTemplateInstantiateRequest{TargetWeekStarts: []string{"2026-06-01"}, ConflictMode: ConflictModeReplace})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	if result.CreatedCount != 3 || result.UnassignedCount != 3 || result.AssignedCount != 0 {
		t.Fatalf("expected all created as unassigned, got %+v", *result)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

type employeeReaderStubWithContractEnd struct {
	base        *employeeReaderStub
	contractEnd map[string]time.Time
}

func (s *employeeReaderStubWithContractEnd) GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*employeespkg.Employee, error) {
	item, err := s.base.GetEmployeeByID(ctx, merchantID, employeeID)
	if err != nil {
		return nil, err
	}
	if end, ok := s.contractEnd[employeeID]; ok {
		item.ContractEndDate = &end
	}
	return item, nil
}

func (s *employeeReaderStubWithContractEnd) GetEmployeePositionByID(ctx context.Context, merchantID, id string) (*employeespkg.EmployeePosition, error) {
	return s.base.GetEmployeePositionByID(ctx, merchantID, id)
}

func (s *employeeReaderStubWithContractEnd) GetEmployeePositionByLabel(ctx context.Context, merchantID, label, excludeID string) (*employeespkg.EmployeePosition, error) {
	return s.base.GetEmployeePositionByLabel(ctx, merchantID, label, excludeID)
}

func TestInstantiateWeekTemplate_IdempotentAndMultiWeekAggregate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}
	defer db.Close()

	empID := "emp-1"
	week1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	week2 := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	weekSource := &weekSourceReaderStub{
		weeksByStart: map[string]*schedulepkg.PlanningWeek{
			"2026-06-01": {ID: "wk-1", MerchantID: "m-1", StartDate: week1},
			"2026-06-08": {ID: "wk-2", MerchantID: "m-1", StartDate: week2},
		},
		shifts: []schedulepkg.PlanningShift{{ID: "same", EmployeeID: &empID, ShiftDate: models.NewDateOnly(week1), StartTime: "09:00:00", EndTime: "11:00:00"}},
	}
	svc := NewService(NewRepository(db), &employeeReaderStub{employees: map[string]bool{"emp-1": true}}, weekSource, &leaveReaderStub{}, nil)

	expectInstantiateTemplateQueries(mock, "wt-3", []WeekTemplateShift{{ID: "ts", DayOfWeek: 1, EmployeeID: &empID, StartTime: "09:00", EndTime: "11:00"}})
	mock.ExpectBegin()
	mock.ExpectCommit()

	result, err := svc.InstantiateWeekTemplate(withPlanningContext(), "wt-3", WeekTemplateInstantiateRequest{TargetWeekStarts: []string{"2026-06-08", "2026-06-01", "2026-06-01"}, ConflictMode: ConflictModeKeepExisting})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	if len(result.PerWeek) != 2 {
		t.Fatalf("expected 2 normalized weeks, got %d", len(result.PerWeek))
	}
	if result.PerWeek[0].TargetWeekStart != "2026-06-01" || result.PerWeek[1].TargetWeekStart != "2026-06-08" {
		t.Fatalf("expected sorted normalized week starts, got %+v", result.PerWeek)
	}
	if result.SkippedCount != 1 || result.CreatedCount != 1 || result.AssignedCount != 1 {
		t.Fatalf("unexpected aggregate counters: %+v", *result)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestInstantiateWeekTemplate_RollbackOnCreateFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}
	defer db.Close()

	empID := "emp-1"
	weekStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	weekSource := &weekSourceReaderStub{
		weeksByStart:   map[string]*schedulepkg.PlanningWeek{"2026-06-01": {ID: "wk-1", MerchantID: "m-1", StartDate: weekStart}},
		createShiftErr: sql.ErrConnDone,
	}
	svc := NewService(NewRepository(db), &employeeReaderStub{employees: map[string]bool{"emp-1": true}}, weekSource, &leaveReaderStub{}, nil)

	expectInstantiateTemplateQueries(mock, "wt-4", []WeekTemplateShift{{ID: "ts", DayOfWeek: 1, EmployeeID: &empID, StartTime: "09:00", EndTime: "11:00"}})
	mock.ExpectBegin()
	mock.ExpectRollback()

	_, err = svc.InstantiateWeekTemplate(withPlanningContext(), "wt-4", WeekTemplateInstantiateRequest{TargetWeekStarts: []string{"2026-06-01"}, ConflictMode: ConflictModeReplace})
	if err == nil {
		t.Fatal("expected instantiate failure")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
