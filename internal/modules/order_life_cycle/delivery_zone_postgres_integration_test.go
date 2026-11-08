//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/modules/orders"
)

// Zone de livraison du client (docs/zones-livraison.md) : recalculée à chaque
// commande en livraison à l'adresse habituelle, depuis les coordonnées à jour ;
// une adresse ponctuelle ne touche pas la zone enregistrée mais la relecture de
// la commande renvoie la zone de cette adresse.
func TestDeliveryZone_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const siret = "siret-olc-zone"
	merchantLat, merchantLng := 49.1193, 6.1757 // Metz

	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM order_comments WHERE order_id IN (SELECT order_id FROM orders WHERE merchant_id = $1)`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM customer WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_Id = $1`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id::text = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = $1 LIMIT 1`, siret).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var mid int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
		VALUES ('ITest OLC Zone', 'a', '1', 's', '57000', 'Metz', $1, 'https://x', '06', 'mtok-olc-zone', 'Europe/Paris', $2, $3)
		RETURNING id`, siret, merchantLat, merchantLng).Scan(&mid); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(mid, 10)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update, zoning_type, cardinal_cone_count, cardinal_zone_ranges)
		VALUES ($1, now(), 'CARDINAL', 4, '0-1,1-3,3-999')`, merchantID); err != nil {
		t.Fatalf("seed params: %v", err)
	}
	var prod int64
	if err := db.QueryRowContext(ctx, `INSERT INTO products (merchant_Id, name, price, category, status) VALUES ($1, 'itest-zone', 1000, 'c', '1') RETURNING product_id`, merchantID).Scan(&prod); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	repo := NewOrdersLifeCycleRepository(db, customers.NewCustomerRepository(db))
	fetcher := orders.NewOrdersFetcher(db)

	// at : position à distanceKm du restaurant, cap en degrés depuis le nord.
	at := func(bearing, distanceKm float64) (float64, float64) {
		rad := bearing * math.Pi / 180
		east, north := distanceKm*math.Sin(rad), distanceKm*math.Cos(rad)
		return merchantLat + north/111.32, merchantLng + east/(111.32*math.Cos(merchantLat*math.Pi/180))
	}

	var customerID *string
	seq := 0
	order := func(t *testing.T, orderType string, temporary bool, lat, lng *float64) string {
		t.Helper()
		seq++
		createdBy := "itest-zone"
		register := "itest-zone"
		tel := "0611223344"
		name := "Client Zone"
		address := fmt.Sprintf("%d rue du Test, Metz", seq)
		req := &models.RequestObject{
			MerchantID: merchantID,
			Order: models.OrderRequest{
				Brand: models.BrandWelloResto, CashRegisterId: &register, CreatedBy: &createdBy,
				TTC: 1000, OrderType: orderType, UseCustomerTemporaryAddress: temporary,
				Customer: &models.CustomerRequest{
					CustomerID: customerID, Name: &name, Tel: &tel, Address: &address, Lat: lat, Lng: lng,
				},
				Products: []models.OrderProductPayload{{ProductID: strconv.FormatInt(prod, 10), ProductName: "Z", Quantity: 1, Price: 1000}},
			},
		}
		res, err := repo.CreateOrder(ctx, req)
		if err != nil || res == nil || res.Status != "success" {
			t.Fatalf("CreateOrder = (%+v, %v)", res, err)
		}
		if customerID == nil {
			customerID = req.Order.Customer.CustomerID
		}
		return res.OrderID
	}
	zone := func(t *testing.T) string {
		t.Helper()
		var code sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT customer_zone_code FROM customer WHERE customer_id::text = $1`, *customerID).Scan(&code); err != nil {
			t.Fatalf("read zone: %v", err)
		}
		return code.String
	}
	fetchedZone := func(t *testing.T, orderID string) string {
		t.Helper()
		list, err := fetcher.FetchAndBuildOrders(ctx, merchantID, orders.QueryFilter{SQL: " AND o.order_id = ?", Args: []interface{}{orderID}}, "", "")
		if err != nil || len(list) != 1 || list[0].Customer == nil {
			t.Fatalf("fetch order %s: %d orders, err %v", orderID, len(list), err)
		}
		if list[0].Customer.CustomerZoneCode == nil {
			return ""
		}
		return *list[0].Customer.CustomerZoneCode
	}
	ptr := func(lat, lng float64) (*float64, *float64) { return &lat, &lng }

	t.Run("livraison : zone calculée et relue avec la commande", func(t *testing.T) {
		lat, lng := ptr(at(90, 2))
		id := order(t, models.OrderTypeDelivery, false, lat, lng)
		if got := zone(t); got != "E2" {
			t.Fatalf("zone = %q, want E2", got)
		}
		if got := fetchedZone(t, id); got != "E2" {
			t.Fatalf("zone relue = %q, want E2", got)
		}
	})

	t.Run("nouvelle adresse : zone et coordonnées mises à jour", func(t *testing.T) {
		lat, lng := ptr(at(0, 0.5))
		order(t, models.OrderTypeDelivery, false, lat, lng)
		if got := zone(t); got != "N1" {
			t.Fatalf("zone = %q, want N1", got)
		}
		var storedLat float64
		if err := db.QueryRowContext(ctx, `SELECT customer_lat FROM customer WHERE customer_id::text = $1`, *customerID).Scan(&storedLat); err != nil || math.Abs(storedLat-*lat) > 1e-9 {
			t.Fatalf("customer_lat = %v (err %v), want %v", storedLat, err, *lat)
		}
	})

	t.Run("à emporter sans coordonnées : zone inchangée", func(t *testing.T) {
		order(t, "TAKE_AWAY", false, nil, nil)
		if got := zone(t); got != "N1" {
			t.Fatalf("zone = %q, want N1 (inchangée)", got)
		}
	})

	t.Run("à emporter avec coordonnées : la zone suit les coordonnées enregistrées", func(t *testing.T) {
		lat, lng := ptr(at(180, 5))
		order(t, "TAKE_AWAY", false, lat, lng)
		if got := zone(t); got != "S3" {
			t.Fatalf("zone = %q, want S3", got)
		}
		lat, lng = ptr(at(0, 0.5))
		order(t, models.OrderTypeDelivery, false, lat, lng)
		if got := zone(t); got != "N1" {
			t.Fatalf("zone = %q, want N1", got)
		}
	})

	t.Run("adresse ponctuelle : zone habituelle intacte, zone ponctuelle relue", func(t *testing.T) {
		lat, lng := ptr(at(270, 5))
		id := order(t, models.OrderTypeDelivery, true, lat, lng)
		if got := zone(t); got != "N1" {
			t.Fatalf("zone enregistrée = %q, want N1 (inchangée)", got)
		}
		if got := fetchedZone(t, id); got != "O3" {
			t.Fatalf("zone relue = %q, want O3", got)
		}
	})

	t.Run("sans coordonnées, la zone suit les coordonnées enregistrées", func(t *testing.T) {
		order(t, models.OrderTypeDelivery, false, nil, nil)
		if got := zone(t); got != "N1" {
			t.Fatalf("zone = %q, want N1", got)
		}
	})

	t.Run("hors découpe puis découpe retirée : zone effacée", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `UPDATE merchant_parameters SET cardinal_zone_ranges = '0-0.2' WHERE merchant_id = $1`, merchantID); err != nil {
			t.Fatal(err)
		}
		lat, lng := ptr(at(0, 0.5))
		order(t, models.OrderTypeDelivery, false, lat, lng)
		if got := zone(t); got != "" {
			t.Fatalf("zone = %q, want vide (hors découpe)", got)
		}
		if _, err := db.ExecContext(ctx, `UPDATE merchant_parameters SET zoning_type = NULL, cardinal_zone_ranges = '0-1,1-3,3-999' WHERE merchant_id = $1`, merchantID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE customer SET customer_zone_code = 'W2' WHERE customer_id::text = $1`, *customerID); err != nil {
			t.Fatal(err)
		}
		order(t, models.OrderTypeDelivery, false, lat, lng)
		if got := zone(t); got != "" {
			t.Fatalf("zone = %q, want vide (découpe retirée)", got)
		}
	})
}
