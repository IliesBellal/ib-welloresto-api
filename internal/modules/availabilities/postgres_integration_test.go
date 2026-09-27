//go:build postgres_integration

package availabilities

import (
	"context"
	"strconv"
	"testing"
	"time"

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

// TestGetActiveProductSchedules_Postgres couvre la requête du filtre horaire
// Kiosk/ScanNOrder (ex-scannorder.GetUnavailableProducts, retirée) : seules
// les disponibilités actives restreignent, créneaux lus en heure locale.
func TestGetActiveProductSchedules_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-avail-sched-m1"
	repo := NewAvailabilitiesRepository(db)

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM availabilities_schedules WHERE availability_id IN (SELECT availability_id FROM availabilities WHERE merchant_id = $1)`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM availabilities_products WHERE availability_id IN (SELECT availability_id FROM availabilities WHERE merchant_id = $1)`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM availabilities WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE merchant_Id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	seedProduct := func(name string) string {
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO products (merchant_Id, name, price, category, tva_in_id, tva_take_away_id, tva_delivery_id, is_popular)
			VALUES ($1, $2, 900, 'itest', 0, 0, 0, false) RETURNING product_id`, merchantID, name).Scan(&id); err != nil {
			t.Fatalf("seed product %s: %v", name, err)
		}
		return strconv.FormatInt(id, 10)
	}
	breakfast := seedProduct("itest-croissant")
	noSlot := seedProduct("itest-fantome")
	inactive := seedProduct("itest-inactif")

	msg := "Dispo le matin"
	// Lundi 06:00-11:00, heure locale stockée telle quelle.
	if _, err := repo.Create(ctx, merchantID, CreateAvailabilityRequest{
		Name: "Petit dej", UnavailableMessage: &msg, ProductIDs: []string{breakfast},
		Schedules: []CreateAvailabilityScheduleReq{{DayOfWeek: 1, StartTime: "06:00", EndTime: "11:00"}},
	}); err != nil {
		t.Fatalf("Create breakfast: %v", err)
	}
	// Disponibilité active dont l'unique créneau est désactivé.
	withoutSlot, err := repo.Create(ctx, merchantID, CreateAvailabilityRequest{
		Name: "Sans creneau", UnavailableMessage: &msg, ProductIDs: []string{noSlot},
		Schedules: []CreateAvailabilityScheduleReq{{DayOfWeek: 1, StartTime: "06:00", EndTime: "11:00"}},
	})
	if err != nil {
		t.Fatalf("Create without slot: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE availabilities_schedules SET enabled = false WHERE availability_id = $1`, withoutSlot.AvailabilityID); err != nil {
		t.Fatalf("disable schedule: %v", err)
	}
	// Disponibilité désactivée (toggle « active » du back-office) : ignorée.
	off, err := repo.Create(ctx, merchantID, CreateAvailabilityRequest{
		Name: "Inactive", UnavailableMessage: &msg, ProductIDs: []string{inactive},
		Schedules: []CreateAvailabilityScheduleReq{{DayOfWeek: 1, StartTime: "06:00", EndTime: "11:00"}},
	})
	if err != nil {
		t.Fatalf("Create inactive: %v", err)
	}
	available := false
	if _, err := repo.Update(ctx, merchantID, off.AvailabilityID, UpdateAvailabilityRequest{Available: &available}); err != nil {
		t.Fatalf("Update inactive: %v", err)
	}

	rows, err := repo.GetActiveProductSchedules(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetActiveProductSchedules failed against postgres: %v", err)
	}
	byProduct := map[string][]ProductScheduleRow{}
	for _, row := range rows {
		byProduct[row.ProductID] = append(byProduct[row.ProductID], row)
	}
	if got := byProduct[breakfast]; len(got) != 1 || !got[0].HasSchedule || got[0].DayOfWeek != 1 || got[0].StartTime != "06:00:00" || got[0].EndTime != "11:00:00" || got[0].ProductName != "itest-croissant" {
		t.Fatalf("unexpected breakfast rows: %+v", got)
	}
	if got := byProduct[noSlot]; len(got) != 1 || got[0].HasSchedule {
		t.Fatalf("expected one row without schedule for %s, got %+v", noSlot, got)
	}
	if got := byProduct[inactive]; len(got) != 0 {
		t.Fatalf("inactive availability must not restrict %s, got %+v", inactive, got)
	}

	mondayMorning := time.Date(2026, 9, 21, 7, 0, 0, 0, time.UTC)
	mondayAfternoon := time.Date(2026, 9, 21, 14, 0, 0, 0, time.UTC)
	if u := UnavailableProductsAt(rows, mondayMorning, time.UTC); len(u) != 1 || u[noSlot] == "" {
		t.Fatalf("at 07:00 only %s must be unavailable, got %+v", noSlot, u)
	}
	if u := UnavailableProductsAt(rows, mondayAfternoon, time.UTC); len(u) != 2 || u[breakfast] == "" || u[noSlot] == "" {
		t.Fatalf("at 14:00 %s and %s must be unavailable, got %+v", breakfast, noSlot, u)
	}
}
