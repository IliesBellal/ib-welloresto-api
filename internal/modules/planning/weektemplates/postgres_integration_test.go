//go:build postgres_integration

package weektemplates

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/helpers"
)

func TestWeekTemplatesRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "999905"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_week_template_shifts WHERE week_template_id IN (SELECT id FROM planning_week_templates WHERE merchant_id = $1)`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_week_templates WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)
	tmplID := helpers.GeneratePrefixedID("wtpl")
	shift := WeekTemplateShift{
		ID: helpers.GeneratePrefixedID("wts"), WeekTemplateID: tmplID,
		DayOfWeek: 1, StartTime: "09:00", EndTime: "17:00", BreakMinutes: 60,
	}
	if err := repo.CreateWeekTemplateWithShifts(ctx, WeekTemplate{ID: tmplID, MerchantID: merchantID, Label: "Semaine itest", Active: true}, []WeekTemplateShift{shift}); err != nil {
		t.Fatalf("CreateWeekTemplateWithShifts: %v", err)
	}
	list, err := repo.ListWeekTemplates(ctx, merchantID)
	if err != nil || len(list) != 1 || list[0].ShiftCount != 1 {
		t.Fatalf("ListWeekTemplates = (%+v, %v)", list, err)
	}
	shifts, err := repo.ListWeekTemplateShifts(ctx, merchantID, tmplID)
	if err != nil || len(shifts) != 1 || shifts[0].StartTime != "09:00" {
		t.Fatalf("ListWeekTemplateShifts = (%+v, %v)", shifts, err)
	}
	shift2 := shift
	shift2.ID = helpers.GeneratePrefixedID("wts")
	shift2.DayOfWeek = 2
	if err := repo.UpdateWeekTemplateWithOptionalShifts(ctx, merchantID, tmplID, WeekTemplate{ID: tmplID, MerchantID: merchantID, Label: "Semaine v2", Active: true}, true, []WeekTemplateShift{shift, shift2}); err != nil {
		t.Fatalf("UpdateWeekTemplateWithOptionalShifts: %v", err)
	}
	if shifts, err := repo.ListWeekTemplateShifts(ctx, merchantID, tmplID); err != nil || len(shifts) != 2 {
		t.Fatalf("shifts après remplacement = (%d, %v)", len(shifts), err)
	}
	if err := repo.SoftDeleteWeekTemplate(ctx, merchantID, tmplID); err != nil {
		t.Fatalf("SoftDeleteWeekTemplate: %v", err)
	}
	// le Get ne filtre pas sur active (identique aux deux dialectes) :
	// on vérifie la bascule active = FALSE
	if got, err := repo.GetWeekTemplateByID(ctx, merchantID, tmplID); err != nil || got.Active {
		t.Fatalf("template devrait être inactif après soft delete: (%+v, %v)", got, err)
	}
}
