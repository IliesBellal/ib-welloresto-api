//go:build postgres_integration

package refs

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Vérification réelle de planning/refs contre le Postgres de dev.
func TestRefsRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	cleanup := func() {
		for _, tbl := range []string{"sys_contract_types", "sys_attendance_sources", "sys_planning_event_types"} {
			_, _ = db.ExecContext(ctx, `DELETE FROM `+tbl+` WHERE code = 'itest-ref'`)
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	for _, tbl := range []string{"sys_contract_types", "sys_attendance_sources", "sys_planning_event_types"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO `+tbl+` (code, label, sort_order, active) VALUES ('itest-ref', 'ITest', 999, true)`); err != nil {
			t.Fatalf("seed %s: %v", tbl, err)
		}
	}

	repo := NewRepository(db)
	for name, list := range map[string]func(context.Context) ([]SystemRef, error){
		"ListContractTypes": repo.ListContractTypes, "ListAttendanceSources": repo.ListAttendanceSources, "ListPlanningEventTypes": repo.ListPlanningEventTypes,
	} {
		items, err := list(ctx)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		found := false
		for _, it := range items {
			if it.Code == "itest-ref" && it.Active {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: itest-ref absent (%d)", name, len(items))
		}
	}

	if ok, err := repo.AttendanceSourceExists(ctx, "itest-ref"); err != nil || !ok {
		t.Fatalf("AttendanceSourceExists = (%v, %v)", ok, err)
	}
	if ok, err := repo.AttendanceSourceExists(ctx, "itest-absent"); err != nil || ok {
		t.Fatalf("AttendanceSourceExists(absent) = (%v, %v)", ok, err)
	}
}
