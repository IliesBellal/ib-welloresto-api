package timeentries

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
	settingspkg "welloresto-api/internal/modules/planning/settings"
)

type stubEmployeeReader struct {
	employee         *employeespkg.Employee
	employeeErr      error
	memberEmployeeID string
	memberErr        error
}

func (s stubEmployeeReader) GetEmployeeByID(ctx context.Context, merchantID, employeeID string) (*employeespkg.Employee, error) {
	return s.employee, s.employeeErr
}

func (s stubEmployeeReader) GetEmployeeIDByMemberID(ctx context.Context, merchantID, memberID string) (string, error) {
	return s.memberEmployeeID, s.memberErr
}

type stubSettingsReader struct {
	settings *settingspkg.PlanningSettings
	err      error
}

func (s stubSettingsReader) GetOrCreateSettings(ctx context.Context, merchantID string) (*settingspkg.PlanningSettings, error) {
	return s.settings, s.err
}

func TestServiceGetCurrentEmployeeTimeEntryResolvesCurrentMemberEmployee(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{
		employee:         &employeespkg.Employee{ID: "emp_1"},
		memberEmployeeID: "emp_1",
	}, nil, nil, nil)

	now := time.Now().UTC()
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

	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1", MerchantRightsID: "42"})
	item, err := svc.GetCurrentEmployeeTimeEntry(ctx, "me")
	if err != nil {
		t.Fatalf("GetCurrentEmployeeTimeEntry() error = %v", err)
	}
	if item == nil || item.EmployeeID != "emp_1" {
		t.Fatalf("GetCurrentEmployeeTimeEntry() resolved employee_id = %#v, want emp_1", item)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceListPlanningTimeEntriesRequiresValidRange(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{}, nil, nil, nil)

	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1"})
	_, _, err = svc.ListPlanningTimeEntries(ctx, PlanningTimeEntryListFilters{From: "", To: "2026-05-07"})
	if !errors.Is(err, models.ErrPlanningInvalidDate) {
		t.Fatalf("ListPlanningTimeEntries() error = %v, want %v", err, models.ErrPlanningInvalidDate)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected SQL expectations: %v", err)
	}
}

func TestServiceListPlanningTimeEntriesResolvesCurrentMemberEmployee(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{
		employee:         &employeespkg.Employee{ID: "emp_1"},
		memberEmployeeID: "emp_1",
	}, nil, nil, nil)

	fromDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	toExclusive := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 5, 7, 9, 30, 0, 0, time.UTC)

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

	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1", MerchantRightsID: "42"})
	items, metadata, err := svc.ListPlanningTimeEntries(ctx, PlanningTimeEntryListFilters{From: "2026-05-01", To: "2026-05-07", EmployeeID: "me"})
	if err != nil {
		t.Fatalf("ListPlanningTimeEntries() error = %v", err)
	}
	if len(items) != 1 || items[0].EmployeeID != "emp_1" {
		t.Fatalf("ListPlanningTimeEntries() items = %#v, want one entry for emp_1", items)
	}
	if metadata.TotalItems != 1 || metadata.CurrentPage != 1 || metadata.Limit != 20 {
		t.Fatalf("ListPlanningTimeEntries() pagination = %#v, want total=1 page=1 limit=20", metadata)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceListPlanningTimeEntriesWithoutEmployeeFilter(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{}, nil, nil, nil)

	fromDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	toExclusive := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 5, 6, 8, 15, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(1)
		FROM planning_time_entries
		WHERE merchant_id = ? AND clock_in_at >= ? AND clock_in_at < ? AND enabled = 1
	`)).
		WithArgs("merchant_1", fromDate, toExclusive).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, modified_by, modified_at, modification_reason, created_at, updated_at, deleted_at
		FROM planning_time_entries
		WHERE merchant_id = ? AND clock_in_at >= ? AND clock_in_at < ? AND enabled = 1
		ORDER BY clock_in_at DESC, created_at DESC
		LIMIT ? OFFSET ?
	`)).
		WithArgs("merchant_1", fromDate, toExclusive, 20, 0).
		WillReturnRows(timeEntryRows().AddRow(
			"entry_2", "merchant_1", "emp_2", nil, "pointage", now, nil, nil, nil, nil, nil, nil, now, now, nil,
		))

	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "user_1", MerchantID: "merchant_1"})
	items, metadata, err := svc.ListPlanningTimeEntries(ctx, PlanningTimeEntryListFilters{From: "2026-05-01", To: "2026-05-07"})
	if err != nil {
		t.Fatalf("ListPlanningTimeEntries() error = %v", err)
	}
	if len(items) != 1 || items[0].EmployeeID != "emp_2" {
		t.Fatalf("ListPlanningTimeEntries() items = %#v, want one entry for emp_2", items)
	}
	if metadata.TotalItems != 1 || metadata.CurrentPage != 1 || metadata.Limit != 20 {
		t.Fatalf("ListPlanningTimeEntries() pagination = %#v, want total=1 page=1 limit=20", metadata)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestPlanningTimeEntryJSONIncludesExplicitNullFields(t *testing.T) {
	entry := PlanningTimeEntry{
		ID:                 "entry_1",
		MerchantID:         "merchant_1",
		EmployeeID:         "emp_1",
		ShiftID:            nil,
		AttendanceSource:   "pointage",
		ClockInAt:          time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC),
		ClockOutAt:         nil,
		ClockInNote:        nil,
		ClockOutNote:       nil,
		ModifiedBy:         nil,
		ModifiedAt:         nil,
		ModificationReason: nil,
		CreatedAt:          time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC),
		UpdatedAt:          time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC),
	}

	payload, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	jsonBody := string(payload)
	for _, expected := range []string{
		`"shift_id":null`,
		`"clock_out_at":null`,
		`"clock_in_note":null`,
		`"clock_out_note":null`,
		`"modified_by":null`,
		`"modified_at":null`,
		`"modification_reason":null`,
	} {
		if !strings.Contains(jsonBody, expected) {
			t.Fatalf("json = %s, want %s", jsonBody, expected)
		}
	}
}

func TestServiceCreateEmployeeTimeEntryManualIgnoresOpenEntryRuleForClosedEntry(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{employee: &employeespkg.Employee{ID: "emp_1"}}, nil, stubSettingsReader{settings: &settingspkg.PlanningSettings{AttendanceSource: settingspkg.AttendanceSourcePointage}}, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "manager_1", MerchantID: "merchant_1", Email: "manager@example.com"})

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO planning_time_entries (
			id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, modified_by, modified_at, modification_reason, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`)).
		WithArgs(sqlmock.AnyArg(), "merchant_1", "emp_1", nil, "pointage", time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC), time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), nil, nil, "manager@example.com", sqlmock.AnyArg(), "Oubli de pointage", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	entry, err := svc.CreateEmployeeTimeEntry(ctx, "emp_1", PlanningTimeEntryManualCreateRequest{
		ClockInAt:          "2026-06-01T08:00:00",
		ClockOutAt:         "2026-06-01T12:00:00",
		ModificationReason: "Oubli de pointage",
	})
	if err != nil {
		t.Fatalf("CreateEmployeeTimeEntry() error = %v", err)
	}
	if entry.ClockOutAt == nil {
		t.Fatal("CreateEmployeeTimeEntry() clock_out_at = nil, want closed entry")
	}
	if entry.ModifiedBy == nil || *entry.ModifiedBy != "manager@example.com" {
		t.Fatalf("CreateEmployeeTimeEntry() modified_by = %#v, want manager@example.com", entry.ModifiedBy)
	}
	if entry.ModificationReason == nil || *entry.ModificationReason != "Oubli de pointage" {
		t.Fatalf("CreateEmployeeTimeEntry() modification_reason = %#v, want Oubli de pointage", entry.ModificationReason)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceCreateEmployeeTimeEntryRejectsInvalidRange(t *testing.T) {
	repo := NewRepository(nil)
	svc := NewService(repo, stubEmployeeReader{employee: &employeespkg.Employee{ID: "emp_1"}}, nil, stubSettingsReader{settings: &settingspkg.PlanningSettings{AttendanceSource: settingspkg.AttendanceSourcePointage}}, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "manager_1", MerchantID: "merchant_1", Email: "manager@example.com"})

	_, err := svc.CreateEmployeeTimeEntry(ctx, "emp_1", PlanningTimeEntryManualCreateRequest{
		ClockInAt:          "2026-06-01T12:00:00",
		ClockOutAt:         "2026-06-01T12:00:00",
		ModificationReason: "Correction",
	})
	if !errors.Is(err, models.ErrPlanningTimeEntryInvalidRange) {
		t.Fatalf("CreateEmployeeTimeEntry() error = %v, want %v", err, models.ErrPlanningTimeEntryInvalidRange)
	}
}

func TestServiceCreateEmployeeTimeEntryRejectsPlanningSource(t *testing.T) {
	repo := NewRepository(nil)
	svc := NewService(repo, stubEmployeeReader{employee: &employeespkg.Employee{ID: "emp_1"}}, nil, stubSettingsReader{settings: &settingspkg.PlanningSettings{AttendanceSource: settingspkg.AttendanceSourcePlanning}}, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "manager_1", MerchantID: "merchant_1", Email: "manager@example.com"})

	_, err := svc.CreateEmployeeTimeEntry(ctx, "emp_1", PlanningTimeEntryManualCreateRequest{
		ClockInAt:          "2026-06-01T08:00:00",
		ClockOutAt:         "2026-06-01T12:00:00",
		ModificationReason: "Correction",
	})
	if !errors.Is(err, models.ErrPlanningTimeEntrySourceDisabled) {
		t.Fatalf("CreateEmployeeTimeEntry() error = %v, want %v", err, models.ErrPlanningTimeEntrySourceDisabled)
	}
}

func TestServiceUpdateEmployeeTimeEntryRequiresModificationReason(t *testing.T) {
	repo := NewRepository(nil)
	svc := NewService(repo, stubEmployeeReader{}, nil, nil, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "manager_1", MerchantID: "merchant_1", Email: "manager@example.com"})

	_, err := svc.UpdateEmployeeTimeEntry(ctx, "emp_1", "entry_1", PlanningTimeEntryCorrectionRequest{ClockInAt: stringPtr("2026-06-01T08:00:00")})
	if !errors.Is(err, models.ErrInvalidData) {
		t.Fatalf("UpdateEmployeeTimeEntry() error = %v, want %v", err, models.ErrInvalidData)
	}
}

func TestServiceUpdateEmployeeTimeEntrySetsModifiedFields(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{employee: &employeespkg.Employee{ID: "emp_1"}}, nil, stubSettingsReader{settings: &settingspkg.PlanningSettings{AttendanceSource: settingspkg.AttendanceSourcePointage}}, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "manager_1", MerchantID: "merchant_1", Email: "manager@example.com"})
	now := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)

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
		SET clock_in_at = ?, clock_out_at = ?, clock_in_note = ?, clock_out_note = ?,
			modified_by = ?, modified_at = ?, modification_reason = ?, updated_at = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`)).
		WithArgs(now, time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), nil, nil, "manager@example.com", sqlmock.AnyArg(), "Badge corrigé", sqlmock.AnyArg(), "merchant_1", "entry_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	entry, err := svc.UpdateEmployeeTimeEntry(ctx, "emp_1", "entry_1", PlanningTimeEntryCorrectionRequest{
		ClockOutAt:         stringPtr("2026-06-01T12:00:00"),
		ModificationReason: "Badge corrigé",
	})
	if err != nil {
		t.Fatalf("UpdateEmployeeTimeEntry() error = %v", err)
	}
	if entry.ModifiedBy == nil || *entry.ModifiedBy != "manager@example.com" {
		t.Fatalf("UpdateEmployeeTimeEntry() modified_by = %#v, want manager@example.com", entry.ModifiedBy)
	}
	if entry.ModifiedAt == nil {
		t.Fatal("UpdateEmployeeTimeEntry() modified_at = nil")
	}
	if entry.ModificationReason == nil || *entry.ModificationReason != "Badge corrigé" {
		t.Fatalf("UpdateEmployeeTimeEntry() modification_reason = %#v, want Badge corrigé", entry.ModificationReason)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceUpdateEmployeeTimeEntryRejectsInvalidRange(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{employee: &employeespkg.Employee{ID: "emp_1"}}, nil, stubSettingsReader{settings: &settingspkg.PlanningSettings{AttendanceSource: settingspkg.AttendanceSourcePointage}}, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "manager_1", MerchantID: "merchant_1", Email: "manager@example.com"})
	now := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)

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

	_, err = svc.UpdateEmployeeTimeEntry(ctx, "emp_1", "entry_1", PlanningTimeEntryCorrectionRequest{
		ClockOutAt:         stringPtr("2026-06-01T07:00:00"),
		ModificationReason: "Erreur",
	})
	if !errors.Is(err, models.ErrPlanningTimeEntryInvalidRange) {
		t.Fatalf("UpdateEmployeeTimeEntry() error = %v, want %v", err, models.ErrPlanningTimeEntryInvalidRange)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestServiceDeleteEmployeeTimeEntrySoftDeletesWithReason(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	svc := NewService(repo, stubEmployeeReader{employee: &employeespkg.Employee{ID: "emp_1"}}, nil, stubSettingsReader{settings: &settingspkg.PlanningSettings{AttendanceSource: settingspkg.AttendanceSourcePointage}}, nil)
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "manager_1", MerchantID: "merchant_1", Email: "manager@example.com"})
	now := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, merchant_id, employee_id, shift_id, attendance_source, clock_in_at, clock_out_at,
			clock_in_note, clock_out_note, modified_by, modified_at, modification_reason, created_at, updated_at, deleted_at
		FROM planning_time_entries
		WHERE merchant_id = ? AND id = ? AND enabled = 1
		LIMIT 1
	`)).
		WithArgs("merchant_1", "entry_1").
		WillReturnRows(timeEntryRows().AddRow(
			"entry_1", "merchant_1", "emp_1", nil, "pointage", now, time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), nil, nil, nil, nil, nil, now, now, nil,
		))

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE planning_time_entries
		SET enabled = 0, deleted_at = ?, updated_at = ?, modified_by = ?, modified_at = ?, modification_reason = ?
		WHERE merchant_id = ? AND id = ? AND enabled = 1
	`)).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "manager@example.com", sqlmock.AnyArg(), "Doublon", "merchant_1", "entry_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = svc.DeleteEmployeeTimeEntry(ctx, "emp_1", "entry_1", PlanningTimeEntryDeleteRequest{ModificationReason: "Doublon"})
	if err != nil {
		t.Fatalf("DeleteEmployeeTimeEntry() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func timeEntryRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "merchant_id", "employee_id", "shift_id", "attendance_source", "clock_in_at", "clock_out_at",
		"clock_in_note", "clock_out_note", "modified_by", "modified_at", "modification_reason", "created_at", "updated_at", "deleted_at",
	})
}

func stringPtr(value string) *string {
	return &value
}
