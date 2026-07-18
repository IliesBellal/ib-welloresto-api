//go:build postgres_integration

package allergens

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestListAllergens_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM allergens WHERE allergen_id = $1`, "itest-alg-1")
	}
	cleanup()
	t.Cleanup(cleanup)

	_, err := db.ExecContext(ctx, `
		INSERT INTO allergens (allergen_id, name, code, icon, color)
		VALUES ($1, $2, $3, $4, $5)`, "itest-alg-1", "ITest Gluten", "ITGL", "wheat", "#FFAA00")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	repo := NewRepository(db)
	list, err := repo.ListAllergens(ctx)
	if err != nil {
		t.Fatalf("ListAllergens failed against postgres: %v", err)
	}

	found := false
	for _, a := range list {
		if a.ID == "itest-alg-1" {
			found = true
			if a.Name != "ITest Gluten" || a.Code != "ITGL" {
				t.Fatalf("unexpected row content: %+v", a)
			}
		}
	}
	if !found {
		t.Fatal("seeded allergen not returned by ListAllergens")
	}
}
