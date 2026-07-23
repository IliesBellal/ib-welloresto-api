//go:build postgres_integration

package swaps

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/helpers"
)

// Vérification réelle de planning/swaps contre le Postgres de dev, dont la
// transaction d'approbation (tx brute enveloppée par dbx.Wrap).
func TestSwapsRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "999909"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_shift_swap_requests WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_shifts WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_weeks WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	start := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO planning_weeks (id, merchant_id, start_date, end_date, status, enabled, created_at, updated_at) VALUES ('pwk-sw', $1, $2, $3, 'draft', true, now(), now())`, merchantID, start.Format("2006-01-02"), start.AddDate(0, 0, 6).Format("2006-01-02")); err != nil {
		t.Fatalf("seed week: %v", err)
	}
	for i, emp := range []string{"emp-sw-1", "emp-sw-2"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO planning_shifts (id, merchant_id, week_id, employee_id, shift_date, start_time, end_time, title, status, enabled, created_at, updated_at) VALUES ($1, $2, 'pwk-sw', $3, $4, '09:00', '17:00', '', 'planned', true, now(), now())`, "psh-sw-"+emp, merchantID, emp, start.AddDate(0, 0, i).Format("2006-01-02")); err != nil {
			t.Fatalf("seed shift %s: %v", emp, err)
		}
	}

	repo := NewRepository(db)
	req, err := repo.CreatePlanningShiftSwapRequest(ctx, merchantID, PlanningShiftSwapRequest{
		ID: helpers.GeneratePrefixedID("pss"), MerchantID: merchantID,
		RequesterEmployeeID: "emp-sw-1", RequesterShiftID: "psh-sw-emp-sw-1",
		TargetEmployeeID: "emp-sw-2", TargetShiftID: "psh-sw-emp-sw-2", Status: "pending",
	})
	if err != nil {
		t.Fatalf("CreatePlanningShiftSwapRequest: %v", err)
	}
	if list, total, err := repo.ListPlanningShiftSwapRequests(ctx, merchantID, PlanningShiftSwapRequestListFilters{Status: "pending", Page: 1, PageSize: 10}); err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("ListPlanningShiftSwapRequests = (%d/%d, %v)", len(list), total, err)
	}
	if self, err := repo.ListCurrentEmployeePlanningShiftSwapRequests(ctx, merchantID, "emp-sw-1", "pending"); err != nil || len(self) != 1 {
		t.Fatalf("ListCurrentEmployeePlanningShiftSwapRequests = (%d, %v)", len(self), err)
	}

	// approbation transactionnelle : les deux shifts échangent leurs employés
	now := time.Now().UTC()
	req.ProcessedAt = &now
	if approved, err := repo.ApprovePlanningShiftSwapRequest(ctx, merchantID, req.ID, *req); err != nil || approved.Status != "approved" {
		t.Fatalf("ApprovePlanningShiftSwapRequest = (%+v, %v)", approved, err)
	}
	var emp1 string
	if err := db.QueryRowContext(ctx, `SELECT employee_id FROM planning_shifts WHERE id = 'psh-sw-emp-sw-1'`).Scan(&emp1); err != nil || emp1 != "emp-sw-2" {
		t.Fatalf("shift requester après swap = (%q, %v), want emp-sw-2", emp1, err)
	}
	if err := repo.SoftDeletePlanningShiftSwapRequest(ctx, merchantID, req.ID); err != nil {
		t.Fatalf("SoftDeletePlanningShiftSwapRequest: %v", err)
	}
}
