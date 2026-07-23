//go:build postgres_integration

package revenueforecast

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Vérification réelle de planning/revenueforecast (upsert ON CONFLICT).
func TestRevenueForecastRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "999911"
	cleanup := func() { _, _ = db.ExecContext(ctx, `DELETE FROM planning_revenue_forecasts WHERE merchant_id = $1`, merchantID) }
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if err := repo.Upsert(ctx, merchantID, day, 100000); err != nil {
		t.Fatalf("Upsert(insert): %v", err)
	}
	if err := repo.Upsert(ctx, merchantID, day, 250000); err != nil {
		t.Fatalf("Upsert(update): %v", err)
	}
	var n, amount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), MIN(amount_ht_cents) FROM planning_revenue_forecasts WHERE merchant_id = $1`, merchantID).Scan(&n, &amount); err != nil || n != 1 || amount != 250000 {
		t.Fatalf("après upsert = (%d lignes, %d, %v), want 1/250000", n, amount, err)
	}
	if err := repo.DeleteByDate(ctx, merchantID, day); err != nil {
		t.Fatalf("DeleteByDate: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM planning_revenue_forecasts WHERE merchant_id = $1`, merchantID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("après delete = (%d, %v)", n, err)
	}
}
