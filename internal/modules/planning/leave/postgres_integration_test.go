//go:build postgres_integration

package leave

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/helpers"
)

// Vérification réelle de planning/leave contre le Postgres de dev.
func TestLeaveRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "999908"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_leave_requests WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_shifts WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_weeks WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	req, err := repo.CreatePlanningLeaveRequest(ctx, merchantID, PlanningLeaveRequest{
		ID: helpers.GeneratePrefixedID("plr"), MerchantID: merchantID, EmployeeID: "emp-lv-1",
		LeaveType: "paid", StartDate: start, EndDate: start.AddDate(0, 0, 2), Status: "pending",
	})
	if err != nil {
		t.Fatalf("CreatePlanningLeaveRequest: %v", err)
	}
	list, total, err := repo.ListPlanningLeaveRequests(ctx, merchantID, PlanningLeaveRequestListFilters{EmployeeID: "emp-lv-1", Status: "pending", Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("ListPlanningLeaveRequests = (%d/%d, %v)", len(list), total, err)
	}
	req.Status = "approved"
	if updated, err := repo.UpdatePlanningLeaveRequest(ctx, merchantID, req.ID, *req); err != nil || updated.Status != "approved" {
		t.Fatalf("UpdatePlanningLeaveRequest = (%+v, %v)", updated, err)
	}
	if overlaps, err := repo.ListApprovedLeavesOverlappingRange(ctx, merchantID, []string{"emp-lv-1"}, start, start.AddDate(0, 0, 5)); err != nil || len(overlaps) != 1 {
		t.Fatalf("ListApprovedLeavesOverlappingRange = (%d, %v)", len(overlaps), err)
	}

	// shift affecté sur la période -> conflits de congés
	if _, err := db.ExecContext(ctx, `INSERT INTO planning_weeks (id, merchant_id, start_date, end_date, status, enabled, created_at, updated_at) VALUES ('pwk-lv', $1, $2, $3, 'draft', true, now(), now())`, merchantID, start.Format("2006-01-02"), start.AddDate(0, 0, 6).Format("2006-01-02")); err != nil {
		t.Fatalf("seed week: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO planning_shifts (id, merchant_id, week_id, employee_id, shift_date, start_time, end_time, title, status, enabled, created_at, updated_at) VALUES ('psh-lv', $1, 'pwk-lv', 'emp-lv-1', $2, '09:00', '17:00', '', 'planned', true, now(), now())`, merchantID, start.AddDate(0, 0, 1).Format("2006-01-02")); err != nil {
		t.Fatalf("seed shift: %v", err)
	}
	if n, err := repo.CountEmployeeAssignedShiftsInRange(ctx, merchantID, "emp-lv-1", start, start.AddDate(0, 0, 2)); err != nil || n != 1 {
		t.Fatalf("CountEmployeeAssignedShiftsInRange = (%d, %v)", n, err)
	}
	if shifts, err := repo.ListEmployeeAssignedShiftsInRange(ctx, merchantID, "emp-lv-1", start, start.AddDate(0, 0, 2)); err != nil || len(shifts) != 1 {
		t.Fatalf("ListEmployeeAssignedShiftsInRange = (%d, %v)", len(shifts), err)
	}
	if err := repo.SoftDeletePlanningLeaveRequest(ctx, merchantID, req.ID); err != nil {
		t.Fatalf("SoftDeletePlanningLeaveRequest: %v", err)
	}
}
