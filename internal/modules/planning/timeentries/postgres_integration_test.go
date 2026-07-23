//go:build postgres_integration

package timeentries

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/helpers"
)

// Vérification réelle de planning/timeentries contre le Postgres de dev.
func TestTimeEntriesRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "999907"
	cleanup := func() { _, _ = db.ExecContext(ctx, `DELETE FROM planning_time_entries WHERE merchant_id = $1`, merchantID) }
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)
	clockIn := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	entry, err := repo.CreatePlanningTimeEntry(ctx, merchantID, PlanningTimeEntry{
		ID: helpers.GeneratePrefixedID("pte"), MerchantID: merchantID, EmployeeID: "emp-te-1",
		AttendanceSource: "pointage", ClockInAt: clockIn,
	})
	if err != nil {
		t.Fatalf("CreatePlanningTimeEntry: %v", err)
	}
	if open, err := repo.GetOpenPlanningTimeEntryForEmployee(ctx, merchantID, "emp-te-1"); err != nil || open == nil || open.ID != entry.ID {
		t.Fatalf("GetOpenPlanningTimeEntryForEmployee = (%+v, %v)", open, err)
	}
	note := "fin de service"
	if closed, err := repo.ClosePlanningTimeEntry(ctx, merchantID, entry.ID, clockIn.Add(8*time.Hour), &note); err != nil || closed.ClockOutAt == nil {
		t.Fatalf("ClosePlanningTimeEntry = (%+v, %v)", closed, err)
	}
	list, total, err := repo.ListPlanningTimeEntries(ctx, merchantID, clockIn.AddDate(0, 0, -1), clockIn.AddDate(0, 0, 1), PlanningTimeEntryListFilters{Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("ListPlanningTimeEntries = (%d/%d, %v)", len(list), total, err)
	}
	if list2, total2, err := repo.ListEmployeeTimeEntries(ctx, merchantID, "emp-te-1", PlanningTimeEntryListFilters{From: "2026-07-01", To: "2026-07-31", Page: 1, PageSize: 10}); err != nil || total2 != 1 || len(list2) != 1 {
		t.Fatalf("ListEmployeeTimeEntries = (%d/%d, %v)", len(list2), total2, err)
	}
	entry.ClockInNote = &note
	if updated, err := repo.UpdatePlanningTimeEntry(ctx, merchantID, entry.ID, *entry); err != nil || updated.ClockInNote == nil {
		t.Fatalf("UpdatePlanningTimeEntry = (%+v, %v)", updated, err)
	}
	actor := "mgr-1"
	if err := repo.SoftDeletePlanningTimeEntry(ctx, merchantID, entry.ID, &actor, time.Now().UTC(), &note); err != nil {
		t.Fatalf("SoftDeletePlanningTimeEntry: %v", err)
	}
	if _, err := repo.GetPlanningTimeEntryByID(ctx, merchantID, entry.ID); err == nil {
		t.Fatalf("entrée devrait être soft-deleted")
	}
}
