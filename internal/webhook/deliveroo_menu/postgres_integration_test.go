//go:build postgres_integration

package deliveroo_menu

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestGetBrandIDBySiteID_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "999901"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM integration_deliveroo WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	_, err := db.ExecContext(ctx, `
		INSERT INTO integration_deliveroo (merchant_id, location_id, brand_id)
		VALUES ($1, $2, $3)`, merchantID, "itest-loc-1", "itest-brand-42")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	repo := NewRepository(db)

	brandID, err := repo.GetBrandIDBySiteID(ctx, "itest-loc-1")
	if err != nil {
		t.Fatalf("GetBrandIDBySiteID failed against postgres: %v", err)
	}
	if brandID != "itest-brand-42" {
		t.Fatalf("expected itest-brand-42, got %s", brandID)
	}

	_, err = repo.GetBrandIDBySiteID(ctx, "itest-loc-unknown")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows for unknown location, got %v", err)
	}
}
