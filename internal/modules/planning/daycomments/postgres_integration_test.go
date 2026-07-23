//go:build postgres_integration

package daycomments

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Vérification réelle de planning/daycomments (rapport 26 : schéma cible et
// audit ON UPDATE déjà faits ; conversion dbx effectuée dans ce chantier).
func TestDayCommentsRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "999912"
	cleanup := func() { _, _ = db.ExecContext(ctx, `DELETE FROM planning_day_comments WHERE merchant_id = $1`, merchantID) }
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)
	day := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)

	created, err := repo.Upsert(ctx, merchantID, day, "grosse résa le soir", "user-1")
	if err != nil || created.Comment != "grosse résa le soir" {
		t.Fatalf("Upsert(insert) = (%+v, %v)", created, err)
	}
	// second upsert même date -> ON CONFLICT, l'auteur d'origine survit
	updated, err := repo.Upsert(ctx, merchantID, day, "grosse résa + anniversaire", "user-2")
	if err != nil || updated.Comment != "grosse résa + anniversaire" {
		t.Fatalf("Upsert(update) = (%+v, %v)", updated, err)
	}
	if updated.ID != created.ID {
		t.Fatalf("l'upsert doit conserver la ligne d'origine (%q != %q)", updated.ID, created.ID)
	}
	if updated.CreatedBy == nil || *updated.CreatedBy != "user-1" || updated.UpdatedBy == nil || *updated.UpdatedBy != "user-2" {
		t.Fatalf("auteurs = %+v", updated)
	}

	list, err := repo.ListByDateRange(ctx, merchantID, day.AddDate(0, 0, -1), day.AddDate(0, 0, 1))
	if err != nil || len(list) != 1 {
		t.Fatalf("ListByDateRange = (%d, %v)", len(list), err)
	}
	if got, err := repo.GetByDate(ctx, merchantID, day); err != nil || got == nil {
		t.Fatalf("GetByDate = (%+v, %v)", got, err)
	}
	if err := repo.Delete(ctx, merchantID, day); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.GetByDate(ctx, merchantID, day); err == nil {
		t.Fatalf("GetByDate après delete devrait renvoyer sql.ErrNoRows")
	}
}
