//go:build postgres_integration

package orders

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// Promotions avec et sans restriction horaire, contre un vrai Postgres :
// le créneau (discounts_schedules) n'est évaluable qu'en SQL. Voir
// docs/KIOSK_DECISIONS.md (2026-09-27).
//
//	POSTGRES_URL=postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev \
//	  go test -tags postgres_integration -run 'Postgres' ./internal/modules/orders/

// seedDiscount insère une promotion (PERCENTAGE, cumulable) et renvoie son
// discount_id_new, requis par les tables filles.
func seedDiscount(t *testing.T, db *sql.DB, id, merchantID string, value int, timeLimited, enabled bool, orderType *string, preferredOrder int) int64 {
	t.Helper()
	var idNew int64
	if err := db.QueryRowContext(context.Background(), `
		INSERT INTO discounts (discount_id, merchant_id, discount_name, discount_desc, prefered_order, discount_order_type, discount_code,
			discount_value, discount_unit, min_order_value, min_order_unit, discounted_quantity, is_cumulative, is_time_limited,
			available, enabled, valid_from)
		VALUES ($1, $2, $1, 'itest', $3, $4, NULL, $5, 'PERCENTAGE', 0, NULL, 1, true, $6, true, $7, now() - interval '1 day')
		RETURNING discount_id_new`, id, merchantID, preferredOrder, orderType, value, timeLimited, enabled).Scan(&idNew); err != nil {
		t.Fatalf("seed discount %s: %v", id, err)
	}
	return idNew
}

func seedSchedule(t *testing.T, db *sql.DB, discountID string, discountIDNew int64, day int, from, to string, enabled bool) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO discounts_schedules (discount_id, discount_id_new, day_of_week, available_from, available_to, enabled)
		VALUES ($1, $2, $3, $4, $5, $6)`, discountID, discountIDNew, day, from, to, enabled); err != nil {
		t.Fatalf("seed schedule %s: %v", discountID, err)
	}
}

// TestDiscountTimeRestriction_Postgres : même promotion avec et sans
// restriction horaire (même créneau lundi 11:00–14:00 en base, intervalle
// [début, fin[), évaluée à plusieurs instants locaux via GetDiscounts.
func TestDiscountTimeRestriction_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "itest-disc-sched-m1"

	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM discounts_schedules WHERE discount_id LIKE 'itest-sched-%'`,
			`DELETE FROM discounts WHERE merchant_id = $1`,
		} {
			if _, err := db.ExecContext(ctx, q, merchantID); err != nil {
				_, _ = db.ExecContext(ctx, q)
			}
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	limited := seedDiscount(t, db, "itest-sched-limited", merchantID, 20, true, true, strPtr("IN TAKE_AWAY"), 0)
	seedSchedule(t, db, "itest-sched-limited", limited, 1, "11:00:00", "14:00:00", true)

	// Même créneau en base, mais is_time_limited = false : le créneau est ignoré.
	unlimited := seedDiscount(t, db, "itest-sched-unlimited", merchantID, 20, false, true, strPtr("IN TAKE_AWAY"), 1)
	seedSchedule(t, db, "itest-sched-unlimited", unlimited, 1, "11:00:00", "14:00:00", true)

	// Créneau désactivé : la promotion restreinte n'est jamais active.
	disabledSlot := seedDiscount(t, db, "itest-sched-disabled-slot", merchantID, 20, true, true, strPtr("IN"), 2)
	seedSchedule(t, db, "itest-sched-disabled-slot", disabledSlot, 1, "11:00:00", "14:00:00", false)

	// Deux créneaux qui se chevauchent : une seule ligne attendue.
	twoSlots := seedDiscount(t, db, "itest-sched-two-slots", merchantID, 20, true, true, strPtr("IN"), 3)
	seedSchedule(t, db, "itest-sched-two-slots", twoSlots, 1, "11:00:00", "14:00:00", true)
	seedSchedule(t, db, "itest-sched-two-slots", twoSlots, 1, "12:00:00", "15:00:00", true)

	// Supprimée dans le back-office.
	seedDiscount(t, db, "itest-sched-deleted", merchantID, 20, false, false, strPtr("IN"), 4)

	// « Tous modes » : discount_order_type et min_order_unit NULL.
	seedDiscount(t, db, "itest-sched-all-modes", merchantID, 20, false, true, nil, 5)

	repo := NewOrdersRepository(db, nil)
	at := func(day int, clock string) map[string]bool {
		t.Helper()
		discounts, err := repo.GetDiscounts(ctx, &models.PricingRequest{
			MerchantID: merchantID,
			Time:       "2026-09-21 " + clock,
			DayOfWeek:  day,
		})
		if err != nil {
			t.Fatalf("GetDiscounts(day %d, %s): %v", day, clock, err)
		}
		got := map[string]bool{}
		for _, d := range discounts {
			if got[d.DiscountID] {
				t.Fatalf("GetDiscounts(day %d, %s) renvoie %s deux fois", day, clock, d.DiscountID)
			}
			got[d.DiscountID] = true
		}
		return got
	}

	always := []string{"itest-sched-unlimited", "itest-sched-all-modes"}
	never := []string{"itest-sched-disabled-slot", "itest-sched-deleted"}

	cases := []struct {
		name       string
		day        int
		clock      string
		limitedOn  bool
		twoSlotsOn bool
	}{
		{"lundi 12:00, dans le créneau", 1, "12:00:00", true, true},
		{"lundi 11:00, début inclus", 1, "11:00:00", true, true},
		{"lundi 14:00, fin exclue ([début, fin[)", 1, "14:00:00", false, true},
		{"lundi 14:30, seul le second créneau", 1, "14:30:00", false, true},
		{"lundi 10:59, avant", 1, "10:59:00", false, false},
		{"lundi 15:01, après", 1, "15:01:00", false, false},
		{"mardi 12:00, autre jour", 2, "12:00:00", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := at(tc.day, tc.clock)
			for _, id := range always {
				if !got[id] {
					t.Fatalf("%s doit être active (sans restriction horaire), got %v", id, got)
				}
			}
			for _, id := range never {
				if got[id] {
					t.Fatalf("%s ne doit jamais être active, got %v", id, got)
				}
			}
			if got["itest-sched-limited"] != tc.limitedOn {
				t.Fatalf("itest-sched-limited active = %v, want %v (got %v)", got["itest-sched-limited"], tc.limitedOn, got)
			}
			if got["itest-sched-two-slots"] != tc.twoSlotsOn {
				t.Fatalf("itest-sched-two-slots active = %v, want %v (got %v)", got["itest-sched-two-slots"], tc.twoSlotsOn, got)
			}
		})
	}
}

// TestComputePricing_TimeLimitedVsPermanent_Postgres : calcul complet
// (ComputePricing, heure réelle du merchant Europe/Paris) sur un panier
// A + B + C :
//   - A : promotion restreinte au jour courant (créneau 00:00–23:59:59) -20 %
//   - B : promotion restreinte à un autre jour -50 % (inactive) et promotion
//     permanente -10 % portant un créneau d'un autre jour (ignoré) ;
//   - C : promotion -30 % réservée à l'option obligatoire X, qui offre le
//     supplément X ; ligne de 2 unités, une seule remisée.
func TestComputePricing_TimeLimitedVsPermanent_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM discounts_products_options WHERE discount_id LIKE 'itest-cp-%'`,
			`DELETE FROM discounts_products WHERE discount_id LIKE 'itest-cp-%'`,
			`DELETE FROM discounts_schedules WHERE discount_id LIKE 'itest-cp-%'`,
			`DELETE FROM discounts WHERE merchant_id = $1`,
			`DELETE FROM configurable_attribute_options WHERE configurable_attribute_id = 'itest-cp-attr'`,
			`DELETE FROM products WHERE merchant_Id = $1`,
			`DELETE FROM tva_categories WHERE tva_id IN (9301, 9302, 9303)`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id::text = $1`,
		} {
			if _, err := db.ExecContext(ctx, q, mid); err != nil {
				_, _ = db.ExecContext(ctx, q)
			}
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-itest-cp' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	mustExec := func(desc, query string, args ...interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %s: %v", desc, err)
		}
	}

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
		VALUES ('ITest Pricing Promos', 'a', '1', 's', '75001', 'Paris', 'siret-itest-cp', 'https://x', '06', 'mtok-itest-cp', 'Europe/Paris', 1, 2)
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)
	mustExec("merchant_parameters", `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency, is_open, delivery_fees, delivery_fees_limit, minimum_cart_for_delivery_order)
		VALUES ($1, now(), 'EUR', true, 250, 2000, 0)`, merchantID)
	mustExec("tva_categories", `
		INSERT INTO tva_categories (tva_id, delivery_type, tva_title, tva_desc, tva_rate, show_in_report)
		OVERRIDING SYSTEM VALUE VALUES
		(9301, 'IN', 'ITest TVA 10', 'itest', 10, TRUE),
		(9302, 'TAKE_AWAY', 'ITest TVA 5.5', 'itest', 5.5, TRUE),
		(9303, 'DELIVERY', 'ITest TVA 20', 'itest', 20, TRUE)`)

	newProduct := func(name string) string {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO products (merchant_Id, name, price, price_take_away, price_delivery, category, status, tva_in_id, tva_delivery_id, tva_take_away_id)
			VALUES ($1, $2, 1000, 900, 1100, 'itest', '1', 9301, 9303, 9302)
			RETURNING product_id`, merchantID, name).Scan(&id); err != nil {
			t.Fatalf("seed product %s: %v", name, err)
		}
		return strconv.FormatInt(id, 10)
	}
	prodA, prodB, prodC := newProduct("itest-cp-A"), newProduct("itest-cp-B"), newProduct("itest-cp-C")

	// Option X, prix officiel 150 (le client enverra 1).
	var optionID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, max_quantity, extra_price, enabled)
		VALUES ('itest-cp-attr', 'Supplément X', 1, 150, 1) RETURNING id`).Scan(&optionID); err != nil {
		t.Fatalf("seed option: %v", err)
	}
	optionX := strconv.FormatInt(optionID, 10)

	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("load Europe/Paris: %v", err)
	}
	now := time.Now().In(paris)
	today := int(now.Weekday())
	if today == 0 {
		today = 7
	}
	otherDay := today%7 + 1

	linkProduct := func(discountID string, idNew int64, productID string) {
		t.Helper()
		mustExec("discounts_products "+discountID, `
			INSERT INTO discounts_products (discount_id, discount_id_new, product_id, new_price, enabled)
			VALUES ($1, $2, $3, NULL, true)`, discountID, idNew, productID)
	}

	todayA := seedDiscount(t, db, "itest-cp-today-A", merchantID, 20, true, true, nil, 0)
	seedSchedule(t, db, "itest-cp-today-A", todayA, today, "00:00:00", "23:59:59", true)
	linkProduct("itest-cp-today-A", todayA, prodA)

	otherDayB := seedDiscount(t, db, "itest-cp-otherday-B", merchantID, 50, true, true, nil, 1)
	seedSchedule(t, db, "itest-cp-otherday-B", otherDayB, otherDay, "00:00:00", "23:59:59", true)
	linkProduct("itest-cp-otherday-B", otherDayB, prodB)

	permanentB := seedDiscount(t, db, "itest-cp-permanent-B", merchantID, 10, false, true, nil, 2)
	seedSchedule(t, db, "itest-cp-permanent-B", permanentB, otherDay, "00:00:00", "00:00:01", true)
	linkProduct("itest-cp-permanent-B", permanentB, prodB)

	optionC := seedDiscount(t, db, "itest-cp-option-C", merchantID, 30, false, true, nil, 3)
	linkProduct("itest-cp-option-C", optionC, prodC)
	mustExec("discounts_products_options", `
		INSERT INTO discounts_products_options (discount_id, discount_id_new, product_id, option_id, new_price, is_option_mandatory)
		VALUES ('itest-cp-option-C', $1, $2, $3, 0, true)`, optionC, prodC, optionX)
	// optionC n'applique qu'une unité (discounted_quantity 1, non cumulable).
	mustExec("optionC non cumulable", `UPDATE discounts SET is_cumulative = false WHERE discount_id = 'itest-cp-option-C'`)

	price := func(req *models.PricingRequest) *models.OrderRequest {
		t.Helper()
		resp, err := (&OrdersService{ordersRepo: NewOrdersRepository(db, nil)}).ComputePricing(ctx, req)
		if err != nil {
			t.Fatalf("ComputePricing: %v", err)
		}
		if resp.Status != "success" || len(resp.UnavailableProduct) != 0 {
			t.Fatalf("unexpected pricing response: %+v", resp)
		}
		return resp.OrderRequest.Order
	}
	optionConfig := func(selected bool) *models.ProductConfiguration {
		return &models.ProductConfiguration{Attributes: []models.ConfigurationAttribute{{
			ID:      "itest-cp-attr",
			Options: []models.ConfigurationOption{{ID: optionX, Selected: selected, ExtraPrice: 1}},
		}}}
	}

	t.Run("créneau du jour actif, autre jour inactif, permanente active", func(t *testing.T) {
		order := price(&models.PricingRequest{
			MerchantID: merchantID,
			Order: &models.OrderRequest{OrderType: "IN", Products: []models.OrderProductPayload{
				{ProductID: prodA, Quantity: 1},
				{ProductID: prodB, Quantity: 1},
			}},
		})
		byProduct := map[string]models.OrderProductPayload{}
		for _, p := range order.Products {
			byProduct[p.ProductID] = p
		}
		a, b := byProduct[prodA], byProduct[prodB]
		if a.DiscountedPrice == nil || *a.DiscountedPrice != 800 || *a.DiscountID != "itest-cp-today-A" {
			t.Fatalf("A doit être remisé à 800 par la promotion du jour, got %+v", a)
		}
		if b.DiscountedPrice == nil || *b.DiscountedPrice != 900 || *b.DiscountID != "itest-cp-permanent-B" {
			t.Fatalf("B doit être remisé à 900 par la promotion permanente (pas -50 %% d'un autre jour), got %+v", b)
		}
		if order.TTC != 1700 {
			t.Fatalf("TTC = %d, want 1700", order.TTC)
		}
	})

	t.Run("option obligatoire choisie : remise, supplément offert, prix officiel ailleurs", func(t *testing.T) {
		cfg := optionConfig(true)
		order := price(&models.PricingRequest{
			MerchantID: merchantID,
			Order: &models.OrderRequest{OrderType: "IN", Products: []models.OrderProductPayload{
				{ProductID: prodC, Quantity: 2, Config: cfg},
			}},
		})
		// Unité remisée : 700 + supplément offert 0 ; autre unité : 1000 + 150
		// (prix officiel, pas le 1 envoyé par le client).
		if order.TTC != 700+1000+150 {
			t.Fatalf("TTC = %d, want %d (lignes : %+v)", order.TTC, 700+1000+150, order.Products)
		}
	})

	t.Run("option obligatoire non choisie : pas de remise", func(t *testing.T) {
		order := price(&models.PricingRequest{
			MerchantID: merchantID,
			Order: &models.OrderRequest{OrderType: "IN", Products: []models.OrderProductPayload{
				{ProductID: prodC, Quantity: 1, Config: optionConfig(false)},
			}},
		})
		if len(order.Products) != 1 || order.Products[0].DiscountedPrice != nil || order.TTC != 1000 {
			t.Fatalf("aucune remise attendue sans l'option obligatoire, got TTC %d, lignes %+v", order.TTC, order.Products)
		}
	})
}
