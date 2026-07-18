//go:build postgres_integration

package googlemaps

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestRecordGoogleMapsCall_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-gmaps-m1"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant_google_maps_monthly WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewGoogleMapsRepository(db)

	// INSERT puis branche DO UPDATE
	if err := repo.RecordGoogleMapsCall(ctx, merchantID, 4); err != nil {
		t.Fatalf("RecordGoogleMapsCall (insert) failed against postgres: %v", err)
	}
	if err := repo.RecordGoogleMapsCall(ctx, merchantID, 6); err != nil {
		t.Fatalf("RecordGoogleMapsCall (upsert) failed against postgres: %v", err)
	}

	var callCount int
	err := db.QueryRowContext(ctx, `
		SELECT call_count FROM merchant_google_maps_monthly WHERE merchant_id = $1`,
		merchantID).Scan(&callCount)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if callCount != 10 {
		t.Fatalf("expected call_count 10, got %d", callCount)
	}
}
