//go:build postgres_integration

package performance

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	statspkg "welloresto-api/internal/modules/stats"
)

type stubStats struct{ tz string }

func (s stubStats) GetMerchantTimezone(ctx context.Context, merchantID string) (string, error) {
	return s.tz, nil
}
func (s stubStats) ListRevenueHTByLocalDay(ctx context.Context, merchantID, tzOffset string, startUTC, endUTC time.Time) ([]statspkg.RevenueHTByLocalDay, error) {
	return []statspkg.RevenueHTByLocalDay{}, nil
}

// Vérification réelle de planning/performance contre le Postgres de dev —
// heures planifiées (date+time), heures pointées (AT TIME ZONE ?::interval,
// écart n°1 du rapport 25) et prévisionnel de CA.
func TestPerformanceRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "999910"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_time_entries WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_shifts WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_weeks WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM planning_revenue_forecasts WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	day := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO planning_weeks (id, merchant_id, start_date, end_date, status, enabled, created_at, updated_at) VALUES ('pwk-pf', $1, $2, $3, 'draft', true, now(), now())`, merchantID, day.Format("2006-01-02"), day.AddDate(0, 0, 6).Format("2006-01-02")); err != nil {
		t.Fatalf("seed week: %v", err)
	}
	// shift de jour 09:00-17:00 avec 60 min de pause -> 420 min, et un shift
	// de nuit 22:00-02:00 (chevauche minuit) -> 240 min
	for _, row := range []struct{ id, start, end string }{{"psh-pf-1", "09:00", "17:00"}, {"psh-pf-2", "22:00", "02:00"}} {
		breakMin := 0
		if row.id == "psh-pf-1" {
			breakMin = 60
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO planning_shifts (id, merchant_id, week_id, employee_id, shift_date, start_time, end_time, break_minutes, title, status, enabled, created_at, updated_at) VALUES ($1, $2, 'pwk-pf', 'emp-pf-1', $3, $4, $5, $6, '', 'planned', true, now(), now())`, row.id, merchantID, day.Format("2006-01-02"), row.start, row.end, breakMin); err != nil {
			t.Fatalf("seed shift: %v", err)
		}
	}
	clockIn := day.Add(9 * time.Hour)
	if _, err := db.ExecContext(ctx, `INSERT INTO planning_time_entries (id, merchant_id, employee_id, attendance_source, clock_in_at, clock_out_at, enabled, created_at, updated_at) VALUES ('pte-pf-1', $1, 'emp-pf-1', 'pointage', $2, $3, true, now(), now())`, merchantID, clockIn, clockIn.Add(7*time.Hour)); err != nil {
		t.Fatalf("seed time entry: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO planning_revenue_forecasts (id, merchant_id, forecast_date, amount_ht_cents) VALUES ('prf-pf-1', $1, $2, 123400)`, merchantID, day.Format("2006-01-02")); err != nil {
		t.Fatalf("seed forecast: %v", err)
	}

	repo := NewRepository(db, stubStats{tz: "UTC"})
	dayKey := day.Format("2006-01-02")

	planned, err := repo.ListPlannedByDayEmployee(ctx, merchantID, day, day)
	if err != nil || len(planned) != 1 {
		t.Fatalf("ListPlannedByDayEmployee = (%+v, %v)", planned, err)
	}
	if planned[0].LocalDay != dayKey || planned[0].PlannedMinutes != 420+240 {
		t.Fatalf("planned minutes = %+v, want %s / 660", planned[0], dayKey)
	}

	worked, err := repo.ListWorkedRawByDayEmployee(ctx, merchantID, day, day)
	if err != nil || len(worked) != 1 || worked[0].WorkedSeconds != 7*3600 {
		t.Fatalf("ListWorkedRawByDayEmployee = (%+v, %v), want 25200s", worked, err)
	}

	forecast, err := repo.ListRevenueForecastByDay(ctx, merchantID, day, day)
	if err != nil || len(forecast) != 1 || forecast[0].AmountHTCents != 123400 {
		t.Fatalf("ListRevenueForecastByDay = (%+v, %v)", forecast, err)
	}
}
