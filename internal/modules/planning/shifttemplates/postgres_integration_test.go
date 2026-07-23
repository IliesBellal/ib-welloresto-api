//go:build postgres_integration

package shifttemplates

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestShiftTemplatesRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "999904"
	cleanup := func() { _, _ = db.ExecContext(ctx, `DELETE FROM planning_shift_templates WHERE merchant_id = $1`, merchantID) }
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)
	tmpl, err := repo.CreateShiftTemplate(ctx, merchantID, ShiftTemplate{
		Label: "Midi itest", StartTime: "11:00", EndTime: "15:00", BreakMinutes: 30, Color: "#10b981", SortOrder: 10, Active: true,
	})
	if err != nil {
		t.Fatalf("CreateShiftTemplate: %v", err)
	}
	got, err := repo.GetShiftTemplateByID(ctx, merchantID, tmpl.ID)
	if err != nil || got.StartTime != "11:00" || got.EndTime != "15:00" {
		t.Fatalf("GetShiftTemplateByID = (%+v, %v) — formats HH:MM attendus", got, err)
	}
	got.Label = "Soir itest"
	got.Active = false
	if updated, err := repo.UpdateShiftTemplate(ctx, merchantID, tmpl.ID, *got); err != nil || updated.Label != "Soir itest" {
		t.Fatalf("UpdateShiftTemplate = (%+v, %v)", updated, err)
	}
	list, err := repo.ListShiftTemplates(ctx, merchantID)
	if err != nil || len(list) != 1 || list[0].Active {
		t.Fatalf("ListShiftTemplates = (%+v, %v)", list, err)
	}
	if next, err := repo.NextShiftTemplateSortOrder(ctx, merchantID); err != nil || next <= 10 {
		t.Fatalf("NextShiftTemplateSortOrder = (%d, %v)", next, err)
	}
}
