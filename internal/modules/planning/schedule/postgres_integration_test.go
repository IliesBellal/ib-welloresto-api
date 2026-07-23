//go:build postgres_integration

package schedule

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

func TestScheduleRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "999906"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_shifts WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_weeks WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC) // lundi
	week, err := repo.CreatePlanningWeek(ctx, merchantID, PlanningWeek{
		ID: helpers.GeneratePrefixedID("pwk"), MerchantID: merchantID,
		StartDate: start, EndDate: start.AddDate(0, 0, 6), Status: "draft",
	})
	if err != nil {
		t.Fatalf("CreatePlanningWeek: %v", err)
	}
	if got, err := repo.GetPlanningWeekByStartDate(ctx, merchantID, start, ""); err != nil || got.ID != week.ID {
		t.Fatalf("GetPlanningWeekByStartDate = (%+v, %v)", got, err)
	}

	empID := "emp-sched-1"
	shift, err := repo.CreatePlanningShift(ctx, merchantID, PlanningShift{
		ID: helpers.GeneratePrefixedID("psh"), MerchantID: merchantID, WeekID: week.ID,
		EmployeeID: &empID, ShiftDate: models.NewDateOnly(start.AddDate(0, 0, 1)),
		StartTime: "09:00", EndTime: "17:00", BreakMinutes: 60, Status: "planned",
	})
	if err != nil {
		t.Fatalf("CreatePlanningShift: %v", err)
	}
	if list, err := repo.ListPlanningShifts(ctx, merchantID, week.ID); err != nil || len(list) != 1 {
		t.Fatalf("ListPlanningShifts = (%d, %v)", len(list), err)
	}
	if list, err := repo.ListPlanningShiftsByDateRange(ctx, merchantID, start, start.AddDate(0, 0, 6)); err != nil || len(list) != 1 {
		t.Fatalf("ListPlanningShiftsByDateRange = (%d, %v)", len(list), err)
	}
	if list, err := repo.ListEmployeeShiftsByDate(ctx, merchantID, empID, start.AddDate(0, 0, 1), ""); err != nil || len(list) != 1 {
		t.Fatalf("ListEmployeeShiftsByDate = (%d, %v)", len(list), err)
	}
	if view, err := repo.ListPlanningShiftsTeamWeekView(ctx, merchantID, week.ID); err != nil || len(view) != 1 {
		t.Fatalf("ListPlanningShiftsTeamWeekView = (%d, %v)", len(view), err)
	}

	shift.EndTime = "18:00"
	if updated, err := repo.UpdatePlanningShift(ctx, merchantID, shift.ID, *shift); err != nil || updated.EndTime != "18:00" {
		t.Fatalf("UpdatePlanningShift = (%+v, %v)", updated, err)
	}

	now := time.Now().UTC()
	if published, err := repo.PublishPlanningWeek(ctx, merchantID, week.ID, now); err != nil || published.Status != "published" || published.PublishedAt == nil {
		t.Fatalf("PublishPlanningWeek = (%+v, %v)", published, err)
	}
	if unpublished, err := repo.UnpublishPlanningWeek(ctx, merchantID, week.ID); err != nil || unpublished.Status != "draft" {
		t.Fatalf("UnpublishPlanningWeek = (%+v, %v)", unpublished, err)
	}

	if err := repo.SoftDeletePlanningShift(ctx, merchantID, shift.ID); err != nil {
		t.Fatalf("SoftDeletePlanningShift: %v", err)
	}
	if err := repo.SoftDeletePlanningWeek(ctx, merchantID, week.ID); err != nil {
		t.Fatalf("SoftDeletePlanningWeek: %v", err)
	}
	if list, err := repo.ListPlanningWeeks(ctx, merchantID); err != nil || len(list) != 0 {
		t.Fatalf("ListPlanningWeeks après delete = (%d, %v)", len(list), err)
	}
}
