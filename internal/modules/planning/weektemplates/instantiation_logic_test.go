package weektemplates

import (
	"testing"
	"time"

	"welloresto-api/internal/models"
	employeespkg "welloresto-api/internal/modules/planning/employees"
	leavepkg "welloresto-api/internal/modules/planning/leave"
	schedulepkg "welloresto-api/internal/modules/planning/schedule"
)

func TestProjectTemplateShiftToDate_MondayAndSunday(t *testing.T) {
	weekStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) // Monday

	mondayDate, err := projectTemplateShiftToDate(WeekTemplateShift{DayOfWeek: 1}, weekStart)
	if err != nil {
		t.Fatalf("project monday: %v", err)
	}
	if dateISO(mondayDate) != "2026-06-01" {
		t.Fatalf("expected monday 2026-06-01, got %s", dateISO(mondayDate))
	}

	sundayDate, err := projectTemplateShiftToDate(WeekTemplateShift{DayOfWeek: 0}, weekStart)
	if err != nil {
		t.Fatalf("project sunday: %v", err)
	}
	if dateISO(sundayDate) != "2026-06-07" {
		t.Fatalf("expected sunday 2026-06-07, got %s", dateISO(sundayDate))
	}
}

func TestClassifyConflict_OnLeaveAndContractEndedPriority(t *testing.T) {
	empID := "emp-1"
	shift := WeekTemplateShift{DayOfWeek: 1, EmployeeID: &empID, StartTime: "09:00", EndTime: "11:00"}
	projected := ProjectedTemplateShift{ShiftDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), TemplateShift: shift}
	existing := []schedulepkg.PlanningShift{{ID: "sh-overlap", EmployeeID: &empID, ShiftDate: models.NewDateOnly(projected.ShiftDate), StartTime: "09:30", EndTime: "10:30"}}
	leaves := []leavepkg.PlanningLeaveRequest{{EmployeeID: empID, Status: "approved", StartDate: projected.ShiftDate, EndDate: projected.ShiftDate}}
	contractEnd := projected.ShiftDate.AddDate(0, 0, -1)
	employee := &employeespkg.Employee{ID: empID, ContractEndDate: &contractEnd}

	classification := classifyConflict(projected, existing, leaves, employee)
	if classification.Reason == nil || *classification.Reason != ConflictReasonOnLeave {
		t.Fatalf("expected on_leave priority, got %#v", classification.Reason)
	}
	if !classification.ForceUnassigned {
		t.Fatal("expected force_unassigned for on_leave")
	}

	classification = classifyConflict(projected, existing, nil, employee)
	if classification.Reason == nil || *classification.Reason != ConflictReasonContractEnded {
		t.Fatalf("expected contract_ended priority, got %#v", classification.Reason)
	}
}

func TestClassifyConflict_OverlapAndNeed(t *testing.T) {
	empID := "emp-1"
	projectedDate := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	projected := ProjectedTemplateShift{ShiftDate: projectedDate, TemplateShift: WeekTemplateShift{DayOfWeek: 1, EmployeeID: &empID, StartTime: "09:00", EndTime: "11:00"}}
	existing := []schedulepkg.PlanningShift{{ID: "sh-overlap", EmployeeID: &empID, ShiftDate: models.NewDateOnly(projectedDate), StartTime: "10:00", EndTime: "12:00"}}

	classification := classifyConflict(projected, existing, nil, nil)
	if classification.Reason == nil || *classification.Reason != ConflictReasonOverlap {
		t.Fatalf("expected overlap, got %#v", classification.Reason)
	}
	if classification.ExistingShiftID == nil || *classification.ExistingShiftID != "sh-overlap" {
		t.Fatalf("expected existing_shift_id sh-overlap")
	}

	needProjected := ProjectedTemplateShift{ShiftDate: projectedDate, TemplateShift: WeekTemplateShift{DayOfWeek: 1, EmployeeID: nil, StartTime: "09:00", EndTime: "11:00"}}
	classification = classifyConflict(needProjected, existing, nil, nil)
	if classification.Reason != nil || classification.Idempotent || classification.ForceUnassigned {
		t.Fatalf("need shift should not conflict, got %+v", classification)
	}
}

func TestClassifyConflict_IdempotentSkipped(t *testing.T) {
	empID := "emp-1"
	posID := "pos-1"
	projectedDate := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	projected := ProjectedTemplateShift{ShiftDate: projectedDate, TemplateShift: WeekTemplateShift{EmployeeID: &empID, PositionID: &posID, StartTime: "09:00", EndTime: "11:00"}}
	existing := []schedulepkg.PlanningShift{{ID: "same", EmployeeID: &empID, PositionID: &posID, ShiftDate: models.NewDateOnly(projectedDate), StartTime: "09:00:00", EndTime: "11:00:00"}}

	classification := classifyConflict(projected, existing, nil, nil)
	if !classification.Idempotent {
		t.Fatal("expected idempotent classification")
	}
}

func TestBuildPreview_MultiWeeksDistinctAndCounters(t *testing.T) {
	empID := "emp-1"
	week1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	week2 := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	templateShifts := []WeekTemplateShift{
		{DayOfWeek: 1, EmployeeID: &empID, StartTime: "09:00", EndTime: "11:00"},
		{DayOfWeek: 2, EmployeeID: &empID, StartTime: "09:00", EndTime: "11:00"},
		{DayOfWeek: 3, EmployeeID: nil, StartTime: "09:00", EndTime: "11:00"},
	}
	existingByWeek := map[string][]schedulepkg.PlanningShift{
		"2026-06-01": {{ID: "overlap-1", EmployeeID: &empID, ShiftDate: models.NewDateOnly(week1), StartTime: "10:00", EndTime: "12:00"}},
		"2026-06-08": {{ID: "overlap-2", EmployeeID: &empID, ShiftDate: models.NewDateOnly(week2.AddDate(0, 0, 1)), StartTime: "09:30", EndTime: "10:30"}},
	}
	leaves := []leavepkg.PlanningLeaveRequest{{EmployeeID: empID, Status: "approved", StartDate: week1.AddDate(0, 0, 1), EndDate: week1.AddDate(0, 0, 1)}}
	employees := map[string]*employeespkg.Employee{empID: {ID: empID, FirstName: "Alice", LastName: "Martin"}}

	preview, err := buildPreview(templateShifts, []time.Time{week2, week1, week1}, existingByWeek, leaves, employees)
	if err != nil {
		t.Fatalf("build preview: %v", err)
	}
	if len(preview.TargetWeekStarts) != 2 || preview.TargetWeekStarts[0] != "2026-06-01" || preview.TargetWeekStarts[1] != "2026-06-08" {
		t.Fatalf("expected normalized target weeks, got %#v", preview.TargetWeekStarts)
	}
	if preview.ImpactedEmployeeCount != 1 {
		t.Fatalf("expected distinct impacted count 1, got %d", preview.ImpactedEmployeeCount)
	}
	if preview.AutoUnassignedCount != 1 {
		t.Fatalf("expected auto_unassigned_count 1, got %d", preview.AutoUnassignedCount)
	}
	if preview.ToCreateCount != 4 {
		t.Fatalf("expected to_create_count 4, got %d", preview.ToCreateCount)
	}
	if len(preview.Conflicts) != 3 {
		t.Fatalf("expected 3 conflicts, got %d", len(preview.Conflicts))
	}
}

func TestDateInRange_InclusiveLeaveBoundaries(t *testing.T) {
	start := time.Date(2026, 6, 5, 8, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 10, 20, 0, 0, 0, time.UTC)

	if !dateInRange(time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC), start, end) {
		t.Fatal("expected range to include start day")
	}
	if !dateInRange(time.Date(2026, 6, 7, 12, 30, 0, 0, time.UTC), start, end) {
		t.Fatal("expected range to include middle day")
	}
	if !dateInRange(time.Date(2026, 6, 10, 23, 59, 0, 0, time.UTC), start, end) {
		t.Fatal("expected range to include end day")
	}
	if dateInRange(time.Date(2026, 6, 4, 23, 59, 0, 0, time.UTC), start, end) {
		t.Fatal("expected day before range to be excluded")
	}
	if dateInRange(time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC), start, end) {
		t.Fatal("expected day after range to be excluded")
	}
}
