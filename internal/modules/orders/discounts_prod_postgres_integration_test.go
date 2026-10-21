//go:build postgres_integration

package orders

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// Promotions de production contre un vrai Postgres (phase 0 du chantier,
// docs/promotions-moteur-chantier.md § 6) : les créneaux et dates réellement
// configurés (export du 2026-10-17) restent actifs aux bons jours et heures, et
// la promotion la plus utilisée (promotion 3) calcule un panier de bout en bout.
//
//	POSTGRES_URL=... go test -tags postgres_integration -run 'ProdPromotions' ./internal/modules/orders/

// TestProdPromotionsActivation_Postgres : GetDiscounts aux limites des créneaux
// réels. Intervalle [début, fin[ ; la promotion 92 (valid_from postérieure à
// valid_to) n'est jamais active ; la promotion 9 (sans créneau) l'est toujours.
func TestProdPromotionsActivation_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchantID = "itest-prod-act-m1"

	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM discounts_schedules WHERE discount_id LIKE 'itest-pa-%'`,
			`DELETE FROM discounts WHERE merchant_id = $1`,
		} {
			if _, err := db.ExecContext(ctx, q, merchantID); err != nil {
				_, _ = db.ExecContext(ctx, q)
			}
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	type slot struct {
		days     []int
		from, to string
	}
	// GetDiscounts renvoie discount_id_new (chantier ids entiers, étape 2) :
	// labels le ramène au numéro de l'export de production.
	labels := map[string]string{}
	seed := func(id string, orderType *string, limited bool, prio int, slots ...slot) {
		idNew := seedDiscount(t, db, id, merchantID, 20, limited, true, orderType, prio)
		labels[idKey(idNew)] = strings.TrimPrefix(id, "itest-pa-")
		for _, s := range slots {
			for _, d := range s.days {
				seedSchedule(t, db, id, idNew, d, s.from, s.to, true)
			}
		}
	}

	// Les identifiants reprennent les numéros de l'export de production.
	seed("itest-pa-3", strPtr("IN TAKE_AWAY"), true, 0, slot{[]int{1, 2, 3, 4, 5, 6}, "10:45:00", "14:00:00"})
	seed("itest-pa-4", strPtr("IN TAKE_AWAY DELIVERY"), true, 1, slot{[]int{1, 2, 3, 4, 5}, "08:45:00", "13:00:00"})
	seed("itest-pa-5", strPtr("IN TAKE_AWAY DELIVERY"), true, 2, slot{[]int{3, 5}, "15:00:00", "23:59:59"})
	seed("itest-pa-8", strPtr("TAKE_AWAY"), true, 3, slot{[]int{1}, "07:00:00", "23:59:59"})
	seed("itest-pa-9", strPtr("TAKE_AWAY"), false, 4)
	seed("itest-pa-92", strPtr("IN"), false, 5)
	// 3 : valable jusqu'en 2030 ; 92 : dates inversées comme en production.
	for _, q := range []string{
		`UPDATE discounts SET valid_to = '2030-02-28' WHERE discount_id = 'itest-pa-3'`,
		`UPDATE discounts SET valid_from = now() - interval '112 days', valid_to = now() - interval '170 days' WHERE discount_id = 'itest-pa-92'`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed dates: %v", err)
		}
	}

	repo := NewOrdersRepository(db, nil)
	today := time.Now().Format("2006-01-02")
	active := func(day int, clock string) string {
		t.Helper()
		discounts, err := repo.GetDiscounts(ctx, &models.PricingRequest{
			MerchantID: merchantID, Time: today + " " + clock, DayOfWeek: day,
		})
		if err != nil {
			t.Fatalf("GetDiscounts(jour %d, %s): %v", day, clock, err)
		}
		ids := make([]string, 0, len(discounts))
		for _, d := range discounts {
			label, ok := labels[d.DiscountID]
			if !ok {
				t.Fatalf("GetDiscounts renvoie %q, pas un discount_id_new de ce test", d.DiscountID)
			}
			ids = append(ids, label)
		}
		sort.Strings(ids)
		return strings.Join(ids, " ")
	}

	// La 9 (sans créneau) est toujours présente ; la 92 jamais.
	cases := []struct {
		name  string
		day   int
		clock string
		want  string
	}{
		{"lundi 06:59 : rien d'ouvert", 1, "06:59:59", "9"},
		{"lundi 07:00 : la 8 s'ouvre", 1, "07:00:00", "8 9"},
		{"lundi 08:45 : la 4 s'ouvre", 1, "08:45:00", "4 8 9"},
		{"lundi 10:44 : la 3 pas encore", 1, "10:44:59", "4 8 9"},
		{"lundi 10:45 : la 3 s'ouvre (début inclus)", 1, "10:45:00", "3 4 8 9"},
		{"lundi 12:59 : la 4 encore ouverte", 1, "12:59:59", "3 4 8 9"},
		{"lundi 13:00 : la 4 se ferme (fin exclue)", 1, "13:00:00", "3 8 9"},
		{"lundi 14:00 : la 3 se ferme (fin exclue)", 1, "14:00:00", "8 9"},
		{"lundi 23:30 : la 8 jusqu'à minuit", 1, "23:30:00", "8 9"},
		{"mercredi 12:00 : 3 et 4, pas la 5", 3, "12:00:00", "3 4 9"},
		{"mercredi 15:00 : la 5 s'ouvre", 3, "15:00:00", "5 9"},
		{"vendredi 23:00 : la 5", 5, "23:00:00", "5 9"},
		{"samedi 12:00 : seulement la 3", 6, "12:00:00", "3 9"},
		{"samedi 15:00 : plus que la 9", 6, "15:00:00", "9"},
		{"dimanche 12:00 : plus que la 9", 7, "12:00:00", "9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := active(tc.day, tc.clock); got != tc.want {
				t.Fatalf("promotions actives = [%s], want [%s]", got, tc.want)
			}
		})
	}
}

// TestProdPromotionsPromo3_ComputePricing_Postgres : la promotion 3 (prix fixe
// 6 €, cumulable, minimum 2, remisée 2) sur un vrai calcul de prix. Le créneau
// de la base couvre la journée d'aujourd'hui pour que le test ne dépende pas de
// l'heure à laquelle il tourne.
func TestProdPromotionsPromo3_ComputePricing_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM discounts_products WHERE discount_id LIKE 'itest-pp-%'`,
			`DELETE FROM discounts_schedules WHERE discount_id LIKE 'itest-pp-%'`,
			`DELETE FROM discounts WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_Id = $1`,
			`DELETE FROM tva_categories WHERE tva_id IN (9311, 9312, 9313)`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id::text = $1`,
		} {
			if _, err := db.ExecContext(ctx, q, mid); err != nil {
				_, _ = db.ExecContext(ctx, q)
			}
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-itest-pp' LIMIT 1`).Scan(&oldID); err == nil {
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
		VALUES ('ITest Prod Promos', 'a', '1', 's', '75001', 'Paris', 'siret-itest-pp', 'https://x', '06', 'mtok-itest-pp', 'Europe/Paris', 1, 2)
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
		(9311, 'IN', 'ITest TVA 10', 'itest', 10, TRUE),
		(9312, 'TAKE_AWAY', 'ITest TVA 5.5', 'itest', 5.5, TRUE),
		(9313, 'DELIVERY', 'ITest TVA 20', 'itest', 20, TRUE)`)

	// Prix de la production (promotion 3, commerçant 776f8f8e) : 790, 870, 990.
	newProduct := func(name string, price int) string {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO products (merchant_Id, name, price, price_take_away, price_delivery, category, status, tva_in_id, tva_delivery_id, tva_take_away_id)
			VALUES ($1, $2, $3, $3, $3, 'itest', '1', 9311, 9313, 9312)
			RETURNING product_id`, merchantID, name, price).Scan(&id); err != nil {
			t.Fatalf("seed product %s: %v", name, err)
		}
		return strconv.FormatInt(id, 10)
	}
	p790, p870, p990 := newProduct("itest-pp-790", 790), newProduct("itest-pp-870", 870), newProduct("itest-pp-990", 990)

	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("load Europe/Paris: %v", err)
	}
	today := int(time.Now().In(paris).Weekday())
	if today == 0 {
		today = 7
	}

	var idNew int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO discounts (discount_id, merchant_id, discount_name, discount_desc, prefered_order, discount_order_type, discount_code,
			discount_value, discount_unit, min_order_value, min_order_unit, discounted_quantity, is_cumulative, is_time_limited,
			available, enabled, valid_from, valid_to)
		VALUES ('itest-pp-3', $1, 'itest-pp-3', 'itest', 0, 'IN TAKE_AWAY', NULL,
			6, 'NEWPRICE', 2, 'QTY', 2, true, true, true, true, now() - interval '1 day', '2030-02-28')
		RETURNING discount_id_new`, merchantID).Scan(&idNew); err != nil {
		t.Fatalf("seed discount: %v", err)
	}
	seedSchedule(t, db, "itest-pp-3", idNew, today, "00:00:00", "23:59:59", true)
	for _, p := range []string{p790, p870, p990} {
		mustExec("discounts_products", `
			INSERT INTO discounts_products (discount_id, discount_id_new, product_id, new_price, enabled)
			VALUES ('itest-pp-3', $1, $2, 600, true)`, idNew, p)
	}

	price := func(orderType string, products ...models.OrderProductPayload) *models.OrderRequest {
		t.Helper()
		resp, err := (&OrdersService{ordersRepo: NewOrdersRepository(db, nil)}).ComputePricing(ctx, &models.PricingRequest{
			MerchantID: merchantID,
			Order:      &models.OrderRequest{OrderType: orderType, Products: products},
		})
		if err != nil {
			t.Fatalf("ComputePricing: %v", err)
		}
		if resp.Status != "success" || len(resp.UnavailableProduct) != 0 {
			t.Fatalf("unexpected pricing response: %+v", resp)
		}
		return resp.OrderRequest.Order
	}
	line := func(id string, qty int) models.OrderProductPayload {
		return models.OrderProductPayload{ProductID: id, Quantity: qty}
	}

	t.Run("un seul article : minimum 2 non atteint", func(t *testing.T) {
		if got := price("IN", line(p790, 1)).TTC; got != 790 {
			t.Fatalf("TTC = %d, want 790", got)
		}
	})
	t.Run("deux articles : 6 € chacun", func(t *testing.T) {
		if got := price("IN", line(p870, 2)).TTC; got != 1200 {
			t.Fatalf("TTC = %d, want 1200", got)
		}
	})
	t.Run("quatre articles à emporter : 6 € chacun", func(t *testing.T) {
		if got := price("TAKE_AWAY", line(p870, 4)).TTC; got != 2400 {
			t.Fatalf("TTC = %d, want 2400", got)
		}
	})
	t.Run("livraison : mode non couvert, prix catalogue", func(t *testing.T) {
		// Le TTC de livraison inclut d'éventuels frais : on vérifie les lignes.
		order := price("DELIVERY", line(p870, 2))
		for _, p := range order.Products {
			if p.DiscountedPrice != nil || p.Price != 870 {
				t.Fatalf("aucune remise attendue en livraison, got %+v", p)
			}
		}
	})
	t.Run("trois articles du plus cher au moins cher : l'article laissé dépend de l'ordre", func(t *testing.T) {
		// Moteur actuel : les deux premiers du panier à 6 €, le dernier au prix
		// du catalogue (790) → 600 + 600 + 790. Règle cible : les deux plus
		// proches de 6 € (790 et 870) sont remisés → 990 + 600 + 600.
		want := 600 + 600 + 790
		if goldenTargetEngine {
			want = 990 + 600 + 600
		}
		order := price("IN", line(p990, 1), line(p870, 1), line(p790, 1))
		if order.TTC != want {
			t.Fatalf("TTC = %d, want %d (lignes : %+v)", order.TTC, want, order.Products)
		}
	})
}
