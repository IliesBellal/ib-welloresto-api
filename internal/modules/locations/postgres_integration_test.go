//go:build postgres_integration

package locations

import (
	"context"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

func TestLocationsRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "itest-loc-m1"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM order_location WHERE order_id IN (SELECT order_id FROM orders WHERE merchant_id = $1)`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM booked_location WHERE booking_id IN (SELECT booking_id FROM bookings WHERE merchant_id = $1)`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM bookings WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM customer WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM floor_obstacles WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM locations WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM floor_areas WHERE floor_id IN (SELECT id FROM floors WHERE merchant_id = $1)`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM floors WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewLocationsRepository(db)

	// CreateFloor: dbx.InsertReturningID fix (floors.id is an identity column).
	floorID, err := repo.CreateFloor(ctx, merchantID, "ITest Floor 1")
	if err != nil {
		t.Fatalf("CreateFloor failed against postgres: %v", err)
	}
	if floorID == "" {
		t.Fatal("expected a non-empty floor id")
	}

	exists, err := repo.FloorExists(ctx, merchantID, floorID)
	if err != nil {
		t.Fatalf("FloorExists failed against postgres: %v", err)
	}
	if !exists {
		t.Fatal("expected floor to exist")
	}

	if err := repo.UpdateFloor(ctx, merchantID, floorID, "ITest Floor Renamed"); err != nil {
		t.Fatalf("UpdateFloor failed against postgres: %v", err)
	}
	var floorName string
	if err := db.QueryRowContext(ctx, `SELECT name FROM floors WHERE id = $1`, floorID).Scan(&floorName); err != nil {
		t.Fatalf("read back floor name: %v", err)
	}
	if floorName != "ITest Floor Renamed" {
		t.Fatalf("expected renamed floor, got %q", floorName)
	}

	// CreateArea: dbx.InsertReturningID fix (floor_areas.id is an identity column).
	areaID, err := repo.CreateArea(ctx, CreateAreaRequest{
		FloorID: floorID, Name: "ITest Area", StrokeColor: "#000000", Color: "#ffffff",
		X: 1, Y: 2, Angle: 0, Points: []AreaPoint{{X: 0, Y: 0}, {X: 10, Y: 10}},
	})
	if err != nil {
		t.Fatalf("CreateArea failed against postgres: %v", err)
	}
	if areaID == "" {
		t.Fatal("expected a non-empty area id")
	}

	newAreaName := "ITest Area Renamed"
	if err := repo.UpdateArea(ctx, merchantID, areaID, UpdateAreaRequest{Name: &newAreaName}); err != nil {
		t.Fatalf("UpdateArea failed against postgres: %v", err)
	}

	// CreateTable (locations): dbx.InsertReturningID fix (locations.location_id
	// is an identity column).
	locationID, err := repo.CreateTable(ctx, merchantID, floorID, CreateTableRequest{
		LocationName: "T1", Seats: 4, Shape: "round", X: 10, Y: 10, Width: 50, Height: 50, Angle: 0,
	})
	if err != nil {
		t.Fatalf("CreateTable failed against postgres: %v", err)
	}
	if locationID == "" {
		t.Fatal("expected a non-empty location id")
	}

	locationID2, err := repo.CreateTable(ctx, merchantID, floorID, CreateTableRequest{
		LocationName: "T2", Seats: 2, Shape: "square", X: 20, Y: 20, Width: 40, Height: 40, Angle: 0,
	})
	if err != nil {
		t.Fatalf("CreateTable (2nd) failed against postgres: %v", err)
	}

	newName := "T1 Renamed"
	newOrder := 5
	if err := repo.UpdateTable(ctx, merchantID, locationID, UpdateTableRequest{LocationName: &newName, LocationOrder: &newOrder}); err != nil {
		t.Fatalf("UpdateTable failed against postgres: %v", err)
	}

	// GetObstaclesByMerchant / CreateObstacle: dbx.UTCNow() fix + boolean TRUE literal.
	obstacleID, err := repo.CreateObstacle(ctx, merchantID, CreateObstacleRequest{
		FloorID: floorID, Type: ObstacleTypeWall, X: 5, Y: 5, Width: 100, Height: 10, Angle: 0,
	})
	if err != nil {
		t.Fatalf("CreateObstacle failed against postgres: %v", err)
	}
	if obstacleID == "" {
		t.Fatal("expected a non-empty obstacle id")
	}

	obstacle, err := repo.GetObstacleByID(ctx, merchantID, obstacleID)
	if err != nil {
		t.Fatalf("GetObstacleByID failed against postgres: %v", err)
	}
	if obstacle.Type != ObstacleTypeWall || !obstacle.Enabled {
		t.Fatalf("unexpected obstacle: %+v", obstacle)
	}

	obstacles, err := repo.GetObstaclesByMerchant(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetObstaclesByMerchant failed against postgres: %v", err)
	}
	if len(obstacles) != 1 {
		t.Fatalf("expected 1 obstacle, got %d", len(obstacles))
	}

	newAngle := 45.0
	if err := repo.UpdateObstacle(ctx, merchantID, obstacleID, UpdateObstacleRequest{Angle: &newAngle}); err != nil {
		t.Fatalf("UpdateObstacle failed against postgres: %v", err)
	}
	obstacle, err = repo.GetObstacleByID(ctx, merchantID, obstacleID)
	if err != nil {
		t.Fatalf("GetObstacleByID (after update) failed: %v", err)
	}
	if obstacle.Angle != newAngle {
		t.Fatalf("expected angle %v, got %v", newAngle, obstacle.Angle)
	}

	// --- GetLocations: exercises the CASE availability literal, the boolean
	// scan, the "IS TRUE" filters, and the UTC_TIMESTAMP/INTERVAL window fix.
	var customerIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO customer (merchant_id, customer_name, customer_tel)
		VALUES ($1, 'ITest Customer', '+33699990000') RETURNING customer_id`, merchantID).Scan(&customerIntID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	customerID := int64ToString(customerIntID)

	future := time.Now().UTC().Add(2 * time.Hour)
	var bookingIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO bookings (booking_number, merchant_id, customer_id, booking_date_from, booking_date_to, booking_duration, party_size, status, created_by)
		VALUES ('ITL001', $1, $2, $3, $4, 60, 4, 'ACCEPTED', 'itest')
		RETURNING booking_id`, merchantID, customerID, future, future.Add(time.Hour)).Scan(&bookingIntID); err != nil {
		t.Fatalf("seed booking: %v", err)
	}
	locIntID := parseLocationID(t, locationID)
	if _, err := db.ExecContext(ctx, `INSERT INTO booked_location (booking_id, location_id) VALUES ($1, $2)`, bookingIntID, locIntID); err != nil {
		t.Fatalf("seed booked_location: %v", err)
	}

	var orderIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand_status, price, tva, ht, created_by, state)
		VALUES ($1, 1, 'ACCEPTED', 1000, 0, 1000, 'itest', 'OPEN')
		RETURNING order_id`, merchantID).Scan(&orderIntID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	locIntID2 := parseLocationID(t, locationID2)
	if _, err := db.ExecContext(ctx, `INSERT INTO order_location (order_id, location_id) VALUES ($1, $2)`, orderIntID, locIntID2); err != nil {
		t.Fatalf("seed order_location: %v", err)
	}

	res, err := repo.GetLocations(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetLocations failed against postgres: %v", err)
	}
	if len(res.Locations) != 2 {
		t.Fatalf("expected 2 locations, got %d: %+v", len(res.Locations), res.Locations)
	}
	if len(res.Floors) != 1 || len(res.Areas) != 1 || len(res.Obstacles) != 1 {
		t.Fatalf("unexpected floors/areas/obstacles counts: floors=%d areas=%d obstacles=%d", len(res.Floors), len(res.Areas), len(res.Obstacles))
	}

	byID := map[string]models.Location{}
	for _, l := range res.Locations {
		byID[l.LocationID] = l
	}
	t1 := byID[locationID]  // has the seeded booking (booked_location), no open order
	t2 := byID[locationID2] // has the seeded open order (order_location)
	if !t1.Available {
		t.Fatalf("expected table 1 available (no open order attached), got %+v", t1)
	}
	if t2.Available {
		t.Fatalf("expected table 2 unavailable (has an open order attached), got %+v", t2)
	}
	if len(t1.Bookings) != 1 || t1.Bookings[0].BookingNumber != "ITL001" {
		t.Fatalf("expected table 1 to carry the seeded booking (UTC_TIMESTAMP/INTERVAL window fix), got %+v", t1.Bookings)
	}

	// DeleteObstacle / DeleteTable / DeleteArea / DeleteFloor.
	if err := repo.DeleteObstacle(ctx, merchantID, obstacleID); err != nil {
		t.Fatalf("DeleteObstacle failed against postgres: %v", err)
	}
	if err := repo.DeleteTable(ctx, merchantID, locationID); err != nil {
		t.Fatalf("DeleteTable failed against postgres: %v", err)
	}
	if err := repo.DeleteTable(ctx, merchantID, locationID2); err != nil {
		t.Fatalf("DeleteTable (2nd) failed against postgres: %v", err)
	}
	if err := repo.DeleteArea(ctx, merchantID, areaID); err != nil {
		t.Fatalf("DeleteArea failed against postgres: %v", err)
	}
	if err := repo.DeleteFloor(ctx, merchantID, floorID); err != nil {
		t.Fatalf("DeleteFloor failed against postgres: %v", err)
	}
	if err := repo.DeleteFloor(ctx, merchantID, floorID); err != models.ErrFloorNotFound {
		t.Fatalf("expected ErrFloorNotFound deleting already-deleted floor, got %v", err)
	}
}

func int64ToString(v int64) string {
	return strconv.FormatInt(v, 10)
}

func parseLocationID(t *testing.T, s string) int64 {
	t.Helper()
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("expected numeric location id, got %q: %v", s, err)
	}
	return v
}
