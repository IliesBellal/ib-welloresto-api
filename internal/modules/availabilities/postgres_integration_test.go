//go:build postgres_integration

package availabilities

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestAvailabilitiesRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-avail-m1"

	repo := NewAvailabilitiesRepository(db)

	cleanup := func() {
		rows, _ := db.QueryContext(ctx, `SELECT availability_id FROM availabilities WHERE merchant_id = $1`, merchantID)
		var ids []string
		if rows != nil {
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err == nil {
					ids = append(ids, id)
				}
			}
			rows.Close()
		}
		for _, id := range ids {
			_, _ = db.ExecContext(ctx, `DELETE FROM availabilities_schedules WHERE availability_id = $1`, id)
			_, _ = db.ExecContext(ctx, `DELETE FROM availabilities_products WHERE availability_id = $1`, id)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM availabilities WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	msg := "Indisponible"
	created, err := repo.Create(ctx, merchantID, CreateAvailabilityRequest{
		Name:               "ITest Availability 1",
		UnavailableMessage: &msg,
		ProductIDs:         []string{"1001", "1002"},
		Schedules: []CreateAvailabilityScheduleReq{
			{DayOfWeek: 1, StartTime: "09:00", EndTime: "12:00"},
			{DayOfWeek: 2, StartTime: "09:00:00", EndTime: "18:00:00"},
		},
	})
	if err != nil {
		t.Fatalf("Create failed against postgres: %v", err)
	}
	if len(created.ProductIDs) != 2 || len(created.Schedules) != 2 {
		t.Fatalf("unexpected created availability: %+v", created)
	}

	// Second availability so the dynamic IN(...) helper (built with strings.Repeat-style
	// placeholders, rebound by dbx) has to resolve more than one id at once.
	msg2 := "Indisponible 2"
	created2, err := repo.Create(ctx, merchantID, CreateAvailabilityRequest{
		Name:               "ITest Availability 2",
		UnavailableMessage: &msg2,
		ProductIDs:         []string{"1003"},
		Schedules: []CreateAvailabilityScheduleReq{
			{DayOfWeek: 3, StartTime: "10:00", EndTime: "14:00"},
		},
	})
	if err != nil {
		t.Fatalf("Create (2nd) failed against postgres: %v", err)
	}

	// GetAvailabilitiesByMerchant exercises getProductIDsByAvailabilityIDs and
	// getSchedulesByAvailabilityIDs with a 2-element dynamic IN(...) clause.
	list, err := repo.GetAvailabilitiesByMerchant(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetAvailabilitiesByMerchant failed against postgres: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 availabilities, got %d", len(list))
	}
	byID := map[string]Availability{}
	for _, a := range list {
		byID[a.AvailabilityID] = a
	}
	if len(byID[created.AvailabilityID].ProductIDs) != 2 {
		t.Fatalf("expected 2 products mapped for %s, got %+v", created.AvailabilityID, byID[created.AvailabilityID])
	}
	if len(byID[created2.AvailabilityID].Schedules) != 1 {
		t.Fatalf("expected 1 schedule mapped for %s, got %+v", created2.AvailabilityID, byID[created2.AvailabilityID])
	}

	fetched, err := repo.GetAvailabilityByID(ctx, merchantID, created.AvailabilityID)
	if err != nil {
		t.Fatalf("GetAvailabilityByID failed against postgres: %v", err)
	}
	if fetched == nil || fetched.Name != "ITest Availability 1" {
		t.Fatalf("unexpected fetched availability: %+v", fetched)
	}

	forProduct, err := repo.GetAvailabilitiesForProduct(ctx, merchantID, "1001")
	if err != nil {
		t.Fatalf("GetAvailabilitiesForProduct failed against postgres: %v", err)
	}
	if len(forProduct) != 1 || forProduct[0].AvailabilityID != created.AvailabilityID {
		t.Fatalf("unexpected GetAvailabilitiesForProduct result: %+v", forProduct)
	}

	// Update: boolean literal + product/schedule replacement.
	newName := "ITest Availability 1 Renamed"
	available := false
	updated, err := repo.Update(ctx, merchantID, created.AvailabilityID, UpdateAvailabilityRequest{
		Name:       &newName,
		Available:  &available,
		ProductIDs: []string{"1004"},
	})
	if err != nil {
		t.Fatalf("Update failed against postgres: %v", err)
	}
	if updated.Name != newName || updated.Available || len(updated.ProductIDs) != 1 || updated.ProductIDs[0] != "1004" {
		t.Fatalf("unexpected updated availability: %+v", updated)
	}

	// Delete: soft delete (enabled = false), then not found.
	if err := repo.Delete(ctx, merchantID, created2.AvailabilityID); err != nil {
		t.Fatalf("Delete failed against postgres: %v", err)
	}
	gone, err := repo.GetAvailabilityByID(ctx, merchantID, created2.AvailabilityID)
	if err != nil {
		t.Fatalf("GetAvailabilityByID (after delete) failed: %v", err)
	}
	if gone != nil {
		t.Fatalf("expected nil after delete, got %+v", gone)
	}
	if err := repo.Delete(ctx, merchantID, created2.AvailabilityID); err == nil {
		t.Fatal("expected error deleting already-deleted availability")
	}
}
