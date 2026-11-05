//go:build postgres_integration

package locations

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"sync"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// TestPublishPlan_Postgres rejoue, contre un vrai Postgres, les garanties de
// POST /locations/plan/publish : ids temporaires résolus, tout-ou-rien,
// contrôle de révision, ordre des suppressions, sérialisation des publications
// concurrentes. Prérequis : migrations 202 et 203 appliquées.
func TestPublishPlan_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-plan-m1"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM floor_areas WHERE floor_id IN (SELECT id FROM floors WHERE merchant_id = $1)`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM floor_obstacles WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM locations WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM floors WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM floor_plan_revisions WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewLocationsRepository(db)

	count := func(t *testing.T, query string, args ...interface{}) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
			t.Fatalf("count %q: %v", query, err)
		}
		return n
	}
	revision := func(t *testing.T) int64 {
		t.Helper()
		return repo.currentPlanRevision(ctx, merchantID)
	}
	mustPublish := func(t *testing.T, req PublishPlanRequest) *PublishPlanResult {
		t.Helper()
		res, err := repo.PublishPlan(ctx, merchantID, req)
		if err != nil {
			t.Fatalf("PublishPlan failed: %v", err)
		}
		return res
	}

	if got := revision(t); got != 0 {
		t.Fatalf("a merchant that never published must be at revision 0, got %d", got)
	}

	// ---------------------------------------------------------------------
	// 1. Création complète : étage + tables (rattachées à l'étage créé dans
	//    le même lot) + obstacles de nouveaux types + zone.
	// ---------------------------------------------------------------------
	created := mustPublish(t, PublishPlanRequest{
		BaseRevision: 0,
		Floors:       PublishFloors{Create: []PublishFloorCreate{{TempID: "tmp_f1", Name: "Salle"}}},
		Tables: PublishTables{Create: []PublishTableCreate{
			{TempID: "tmp_t1", FloorID: "tmp_f1", LocationName: "Table 1", Seats: 2, Shape: "circle", X: 100, Y: 100, Width: 80, Height: 80},
			{TempID: "tmp_t2", FloorID: "tmp_f1", LocationName: "Table 2", Seats: 4, Shape: "rectangle", X: 300, Y: 100, Width: 120, Height: 80,
				Attributes: &TableAttributes{PMR: true}},
		}},
		Obstacles: PublishObstacles{Create: []PublishObstacleCreate{
			{TempID: "tmp_o1", FloorID: "tmp_f1", Type: ObstacleTypePlant, X: 50, Y: 50, Width: 40, Height: 40},
			{TempID: "tmp_o2", FloorID: "tmp_f1", Type: ObstacleTypeWall, X: 500, Y: 20, Width: 900, Height: 15},
			{TempID: "tmp_o3", FloorID: "tmp_f1", Type: ObstacleTypeWindow, X: 700, Y: 20, Width: 120, Height: 12},
		}},
		Areas: PublishAreas{Create: []PublishAreaCreate{{
			TempID: "tmp_a1", FloorID: "tmp_f1", Name: "Coin", StrokeColor: "#64748B", Color: "#E2E8F0",
			Points: []AreaPoint{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}},
		}}},
	})

	if created.Revision != 1 || revision(t) != 1 {
		t.Fatalf("first publish must bring the plan to revision 1, got result=%d stored=%d", created.Revision, revision(t))
	}
	for _, temp := range []string{"tmp_f1", "tmp_t1", "tmp_t2", "tmp_o1", "tmp_o2", "tmp_o3", "tmp_a1"} {
		if created.IDMap[temp] == "" {
			t.Fatalf("id_map is missing %s: %+v", temp, created.IDMap)
		}
	}
	floorID := created.IDMap["tmp_f1"]
	table1, table2 := created.IDMap["tmp_t1"], created.IDMap["tmp_t2"]
	obstacle1, obstacle2 := created.IDMap["tmp_o1"], created.IDMap["tmp_o2"]
	area1 := created.IDMap["tmp_a1"]

	if _, err := strconv.Atoi(floorID); err != nil {
		t.Fatalf("a floor id must be the generated integer, got %q", floorID)
	}
	if n := count(t, `SELECT COUNT(*) FROM locations WHERE merchant_id = $1 AND floor_id = $2 AND enabled`, merchantID, floorID); n != 2 {
		t.Fatalf("both tables must belong to the floor created in the same batch, got %d", n)
	}
	var seats int
	var pmr bool
	if err := db.QueryRowContext(ctx,
		`SELECT seats, COALESCE((attributes->>'pmr')::boolean, false) FROM locations WHERE location_id = $1`, table2,
	).Scan(&seats, &pmr); err != nil || seats != 4 || !pmr {
		t.Fatalf("table 2 stored seats=%d pmr=%v err=%v", seats, pmr, err)
	}
	for _, typ := range []string{"plant", "wall", "window"} {
		if n := count(t, `SELECT COUNT(*) FROM floor_obstacles WHERE merchant_id = $1 AND type = $2 AND enabled`, merchantID, typ); n != 1 {
			t.Fatalf("expected one %s obstacle, got %d", typ, n)
		}
	}

	// ---------------------------------------------------------------------
	// 2. Mises à jour : renommage d'étage, table déplacée / re-dimensionnée,
	//    obstacle modifié (entité complète).
	// ---------------------------------------------------------------------
	renamed := "Salle principale"
	seats4 := 6
	x := 220.0
	mustPublish(t, PublishPlanRequest{
		BaseRevision: 1,
		Floors:       PublishFloors{Update: []PublishFloorUpdate{{ID: floorID, Name: renamed}}},
		Tables: PublishTables{Update: []PublishTableUpdate{{ID: table1, UpdateTableRequest: UpdateTableRequest{
			Seats: &seats4, X: &x,
		}}}},
		Obstacles: PublishObstacles{Update: []PublishObstacleUpdate{{ID: obstacle1, UpdateObstacleRequest: UpdateObstacleRequest{
			Type: obstacleType(ObstacleTypePlant), X: f64(75),
		}}}},
	})
	var name string
	if err := db.QueryRowContext(ctx, `SELECT name FROM floors WHERE id = $1`, floorID).Scan(&name); err != nil || name != renamed {
		t.Fatalf("floor name = %q, err = %v", name, err)
	}
	var gotSeats int
	var gotX float64
	if err := db.QueryRowContext(ctx, `SELECT seats, current_x FROM locations WHERE location_id = $1`, table1).Scan(&gotSeats, &gotX); err != nil ||
		gotSeats != 6 || gotX != 220 {
		t.Fatalf("table 1 seats=%d x=%v err=%v", gotSeats, gotX, err)
	}
	if revision(t) != 2 {
		t.Fatalf("expected revision 2, got %d", revision(t))
	}

	// ---------------------------------------------------------------------
	// 3. Révision périmée : refusée, rien n'est appliqué.
	// ---------------------------------------------------------------------
	_, err := repo.PublishPlan(ctx, merchantID, PublishPlanRequest{
		BaseRevision: 1,
		Floors:       PublishFloors{Update: []PublishFloorUpdate{{ID: floorID, Name: "Écrasé"}}},
	})
	if !errors.Is(err, models.ErrPlanRevisionConflict) {
		t.Fatalf("stale base_revision must return ErrPlanRevisionConflict, got %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT name FROM floors WHERE id = $1`, floorID).Scan(&name); err != nil || name != renamed {
		t.Fatalf("a rejected publish must not change anything, floor name = %q", name)
	}
	if revision(t) != 2 {
		t.Fatalf("a rejected publish must not bump the revision, got %d", revision(t))
	}

	// ---------------------------------------------------------------------
	// 4. Tout ou rien : un lot dont la fin échoue ne laisse aucune trace.
	// ---------------------------------------------------------------------
	_, err = repo.PublishPlan(ctx, merchantID, PublishPlanRequest{
		BaseRevision: 2,
		Floors:       PublishFloors{Create: []PublishFloorCreate{{TempID: "tmp_f9", Name: "Fantôme"}}},
		Tables: PublishTables{Create: []PublishTableCreate{
			{TempID: "tmp_t9", FloorID: "tmp_f9", LocationName: "Table 9", Seats: 2, Shape: "square", X: 10, Y: 10, Width: 80, Height: 80},
			{TempID: "tmp_t10", FloorID: "987654", LocationName: "Table 10", Seats: 2, Shape: "square", X: 10, Y: 10, Width: 80, Height: 80},
		}},
	})
	if !errors.Is(err, models.ErrFloorNotFound) {
		t.Fatalf("a table on an unknown floor must return ErrFloorNotFound, got %v", err)
	}
	if n := count(t, `SELECT COUNT(*) FROM floors WHERE merchant_id = $1 AND name = 'Fantôme'`, merchantID); n != 0 {
		t.Fatal("the floor created earlier in the failed batch must have been rolled back")
	}
	if n := count(t, `SELECT COUNT(*) FROM locations WHERE merchant_id = $1`, merchantID); n != 2 {
		t.Fatalf("the failed batch must not leave tables behind, got %d", n)
	}
	if revision(t) != 2 {
		t.Fatalf("a failed publish must not bump the revision, got %d", revision(t))
	}

	// ---------------------------------------------------------------------
	// 5. Garde-fous : étage non vide, table inconnue, référence temporaire
	//    orpheline.
	// ---------------------------------------------------------------------
	_, err = repo.PublishPlan(ctx, merchantID, PublishPlanRequest{
		BaseRevision: 2,
		Floors:       PublishFloors{Delete: []string{floorID}},
	})
	if !errors.Is(err, models.ErrFloorNotEmpty) {
		t.Fatalf("deleting a floor that still has tables must return ErrFloorNotEmpty, got %v", err)
	}
	if n := count(t, `SELECT COUNT(*) FROM floors WHERE id = $1 AND enabled`, floorID); n != 1 {
		t.Fatal("the floor must survive a refused deletion")
	}
	if n := count(t, `SELECT COUNT(*) FROM floor_obstacles WHERE merchant_id = $1 AND enabled`, merchantID); n != 3 {
		t.Fatalf("a refused floor deletion must roll back the obstacle cleanup too, got %d enabled obstacles", n)
	}

	_, err = repo.PublishPlan(ctx, merchantID, PublishPlanRequest{
		BaseRevision: 2,
		Tables:       PublishTables{Delete: []string{"987654"}},
	})
	if !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("deleting an unknown table must return ErrNotFound, got %v", err)
	}

	_, err = repo.PublishPlan(ctx, merchantID, PublishPlanRequest{
		BaseRevision: 2,
		Tables: PublishTables{Create: []PublishTableCreate{
			{TempID: "tmp_t11", FloorID: "tmp_unknown", LocationName: "Table 11", Seats: 2, Shape: "square", X: 10, Y: 10, Width: 80, Height: 80},
		}},
	})
	if !errors.Is(err, models.ErrInvalidPlanPayload) {
		t.Fatalf("an unresolved temporary floor reference must return ErrInvalidPlanPayload, got %v", err)
	}

	// Une table ne peut pas pointer vers un temp_id qui n'est pas celui d'un étage.
	_, err = repo.PublishPlan(ctx, merchantID, PublishPlanRequest{
		BaseRevision: 2,
		Tables: PublishTables{Create: []PublishTableCreate{
			{TempID: "tmp_t12", FloorID: "tmp_t12", LocationName: "Table 12", Seats: 2, Shape: "square", X: 10, Y: 10, Width: 80, Height: 80},
		}},
	})
	if !errors.Is(err, models.ErrInvalidPlanPayload) {
		t.Fatalf("a table cannot use a table temp id as its floor, got %v", err)
	}

	// ---------------------------------------------------------------------
	// 6. Déplacer les tables vers un nouvel étage, supprimer l'ancien :
	//    ses obstacles / zones restants sont désactivés avec lui.
	// ---------------------------------------------------------------------
	moved := mustPublish(t, PublishPlanRequest{
		BaseRevision: 2,
		Floors: PublishFloors{
			Create: []PublishFloorCreate{{TempID: "tmp_f2", Name: "Terrasse"}},
			Delete: []string{floorID},
		},
		Tables: PublishTables{
			Update: []PublishTableUpdate{{ID: table1, UpdateTableRequest: UpdateTableRequest{FloorID: strPtr("tmp_f2")}}},
			Delete: []string{table2},
		},
		Obstacles: PublishObstacles{Delete: []string{obstacle1}},
	})
	if moved.Revision != 3 {
		t.Fatalf("expected revision 3, got %d", moved.Revision)
	}
	newFloorID := moved.IDMap["tmp_f2"]
	if n := count(t, `SELECT COUNT(*) FROM locations WHERE location_id = $1 AND floor_id = $2 AND enabled`, table1, newFloorID); n != 1 {
		t.Fatal("table 1 must now belong to the floor created in the same batch")
	}
	if n := count(t, `SELECT COUNT(*) FROM locations WHERE location_id = $1 AND enabled`, table2); n != 0 {
		t.Fatal("table 2 must be soft-deleted")
	}
	if n := count(t, `SELECT COUNT(*) FROM floors WHERE id = $1 AND enabled`, floorID); n != 0 {
		t.Fatal("the old floor must be disabled")
	}
	if n := count(t, `SELECT COUNT(*) FROM floor_obstacles WHERE id = $1 AND enabled`, obstacle2); n != 0 {
		t.Fatal("an obstacle left on a deleted floor must be disabled with it")
	}
	if n := count(t, `SELECT COUNT(*) FROM floor_areas WHERE id = $1 AND enabled`, area1); n != 0 {
		t.Fatal("an area left on a deleted floor must be disabled with it")
	}

	// ---------------------------------------------------------------------
	// 7. Deux publications simultanées sur la même révision : une seule
	//    passe, l'autre voit le conflit (le verrou de révision les sérialise).
	// ---------------------------------------------------------------------
	var wg sync.WaitGroup
	results := make([]error, 2)
	for n := range results {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, results[n] = repo.PublishPlan(ctx, merchantID, PublishPlanRequest{
				BaseRevision: 3,
				Floors:       PublishFloors{Create: []PublishFloorCreate{{TempID: "tmp_fc", Name: "Concurrent " + strconv.Itoa(n)}}},
			})
		}(n)
	}
	wg.Wait()

	succeeded, conflicted := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, models.ErrPlanRevisionConflict):
			conflicted++
		default:
			t.Fatalf("unexpected error from a concurrent publish: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("exactly one concurrent publish must win: succeeded=%d conflicted=%d", succeeded, conflicted)
	}
	if revision(t) != 4 {
		t.Fatalf("expected revision 4 after the winning concurrent publish, got %d", revision(t))
	}
	if n := count(t, `SELECT COUNT(*) FROM floors WHERE merchant_id = $1 AND name LIKE 'Concurrent %'`, merchantID); n != 1 {
		t.Fatalf("only the winning concurrent publish may create its floor, got %d", n)
	}

	// Un autre établissement n'est ni lu ni modifié par ces publications.
	var other sql.NullInt64
	_ = db.QueryRowContext(ctx, `SELECT revision FROM floor_plan_revisions WHERE merchant_id = 'itest-plan-other'`).Scan(&other)
	if other.Valid {
		t.Fatal("publishing must only touch the caller's own revision row")
	}
}

func strPtr(s string) *string { return &s }
