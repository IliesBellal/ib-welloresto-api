//go:build postgres_integration

package settings

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Vérification réelle de planning/settings contre le Postgres de dev
// (settings + jours fériés, y compris ResolvePlanningHoliday utilisé par
// pos.GetPOSStatus et scannorder.GetMerchantStatus).
func TestPlanningSettingsRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "999901"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_holiday_overrides WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_settings WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM holiday_calendar WHERE country_code = 'ZZ'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM labor_rules WHERE country_code = 'ZZ'`)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)

	// GetOrCreateSettings : création avec le défaut FR (labor_rules absent -> défaut codé)
	settings, err := repo.GetOrCreateSettings(ctx, merchantID)
	if err != nil || settings.MerchantID != merchantID || !settings.AllowOverrideWarnings {
		t.Fatalf("GetOrCreateSettings = (%+v, %v)", settings, err)
	}
	if settings.PlanningSMSNotificationsEnabled {
		t.Fatal("expected planning_sms_notifications_enabled to default to false")
	}
	// idempotent
	if again, err := repo.GetOrCreateSettings(ctx, merchantID); err != nil || again.ID != settings.ID {
		t.Fatalf("GetOrCreateSettings(2e) = (%+v, %v)", again, err)
	}

	code := "ZZ"
	if _, err := db.ExecContext(ctx, `INSERT INTO labor_rules (country_code, label, min_daily_rest_hours, min_break_minutes, night_shift_start, night_shift_end, night_shift_multiplier, holiday_multiplier, max_weekly_hours, created_at, updated_at) VALUES ('ZZ', 'Testland', 10, 30, '21:00:00', '05:00:00', 1.5, 2, 40, now(), now())`); err != nil {
		t.Fatalf("seed labor_rules: %v", err)
	}
	if rule, err := repo.GetLaborRuleByCountry(ctx, "zz"); err != nil || rule.MaxWeeklyHours != 40 {
		t.Fatalf("GetLaborRuleByCountry = (%+v, %v)", rule, err)
	}

	updated, err := repo.UpdateSettings(ctx, merchantID, PlanningSettingsUpdateRequest{LaborCountryCode: &code})
	if err != nil || updated.LaborCountryCode != "ZZ" {
		t.Fatalf("UpdateSettings = (%+v, %v)", updated, err)
	}

	// jours fériés : calendrier légal + override marchand
	holidayDate := time.Date(2026, 12, 25, 0, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO holiday_calendar (id, country_code, holiday_date, label) VALUES ('itest-hc-1', 'ZZ', '2026-12-25', 'Noël itest')`); err != nil {
		t.Fatalf("seed holiday_calendar: %v", err)
	}

	holiday, err := repo.ResolvePlanningHoliday(ctx, merchantID, holidayDate)
	if err != nil || !holiday.IsLegalHoliday || !holiday.CountAsHoliday {
		t.Fatalf("ResolvePlanningHoliday(légal) = (%+v, %v)", holiday, err)
	}
	// jour ordinaire
	if h, err := repo.ResolvePlanningHoliday(ctx, merchantID, holidayDate.AddDate(0, 0, 1)); err != nil || h.IsLegalHoliday || h.CountAsHoliday {
		t.Fatalf("ResolvePlanningHoliday(ordinaire) = (%+v, %v)", h, err)
	}

	closed := false
	if _, err := repo.CreatePlanningHolidayOverride(ctx, merchantID, planningHolidayOverrideRecord{HolidayDate: holidayDate, IsOpen: &closed}); err != nil {
		t.Fatalf("CreatePlanningHolidayOverride: %v", err)
	}
	holiday, err = repo.ResolvePlanningHoliday(ctx, merchantID, holidayDate)
	if err != nil || holiday.IsOpen == nil || *holiday.IsOpen {
		t.Fatalf("ResolvePlanningHoliday(override fermé) = (%+v, %v)", holiday, err)
	}

	list, err := repo.ListPlanningHolidays(ctx, merchantID, holidayDate.AddDate(0, 0, -1), holidayDate.AddDate(0, 0, 1))
	if err != nil || len(list) != 1 || list[0].OverrideID == nil {
		t.Fatalf("ListPlanningHolidays = (%+v, %v)", list, err)
	}

	open := true
	if _, err := repo.UpdatePlanningHolidayOverride(ctx, merchantID, planningHolidayOverrideRecord{HolidayDate: holidayDate, IsOpen: &open}); err != nil {
		t.Fatalf("UpdatePlanningHolidayOverride: %v", err)
	}
	if rec, err := repo.GetPlanningHolidayOverrideByDate(ctx, merchantID, holidayDate); err != nil || rec.IsOpen == nil || !*rec.IsOpen {
		t.Fatalf("GetPlanningHolidayOverrideByDate = (%+v, %v)", rec, err)
	}
	if err := repo.SoftDeletePlanningHolidayOverride(ctx, merchantID, holidayDate); err != nil {
		t.Fatalf("SoftDeletePlanningHolidayOverride: %v", err)
	}
}
