package orders

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"
)

// Couvre le calcul des promotions du pricing (orders.ComputePricing), commun
// au POS, à ScanNOrder et à la borne : règles d'éligibilité et de calcul de
// applyDiscounts, lecture des promotions (GetDiscounts), et un calcul complet
// de bout en bout. Voir docs/KIOSK_DECISIONS.md (2026-09-27).

type unitSpec struct {
	productID string
	price     int
	qty       int
}

// units reproduit l'éclatement de buildSelectedProducts : une ligne par unité.
func units(specs ...unitSpec) ([]models.OrderProductPayload, int) {
	var out []models.OrderProductPayload
	total := 0
	for _, s := range specs {
		for i := 0; i < s.qty; i++ {
			out = append(out, models.OrderProductPayload{ProductID: s.productID, Price: s.price, Quantity: 1})
		}
		total += s.price * s.qty
	}
	return out, total
}

// unitPrices renvoie le prix remisé de chaque unité, -1 si non remisée.
func unitPrices(products []models.OrderProductPayload) []int {
	out := make([]int, 0, len(products))
	for _, p := range products {
		if p.DiscountedPrice == nil {
			out = append(out, -1)
		} else {
			out = append(out, *p.DiscountedPrice)
		}
	}
	return out
}

func strPtr(s string) *string   { return &s }
func intPtr(i int) *int         { return &i }
func f64Ptr(f float64) *float64 { return &f }
func targeted(ids ...string) map[string]*models.DiscountProductInfo {
	out := map[string]*models.DiscountProductInfo{}
	for _, id := range ids {
		out[id] = &models.DiscountProductInfo{ProductID: id}
	}
	return out
}

func TestApplyDiscounts(t *testing.T) {
	pct := func(id string, value int) *models.DBDiscount {
		return &models.DBDiscount{DiscountID: id, DiscountUnit: "PERCENTAGE", DiscountValue: value, DiscountedQuantity: 1, Available: true}
	}

	cases := []struct {
		name        string
		orderType   string
		code        string
		products    []unitSpec
		discounts   []*models.DBDiscount
		dp          map[string]map[string]*models.DiscountProductInfo
		wantPrices  []int
		wantApplied []string
	}{
		{
			name:        "pourcentage sur tout le panier, non cumulable : une seule unité",
			orderType:   "IN",
			products:    []unitSpec{{"A", 1000, 2}},
			discounts:   []*models.DBDiscount{pct("d1", 20)},
			wantPrices:  []int{800, -1},
			wantApplied: []string{"d1"},
		},
		{
			name:        "pourcentage ciblé : seul le produit visé",
			orderType:   "IN",
			products:    []unitSpec{{"A", 1000, 1}, {"B", 500, 1}},
			discounts:   []*models.DBDiscount{pct("d1", 20)},
			dp:          map[string]map[string]*models.DiscountProductInfo{"d1": targeted("B")},
			wantPrices:  []int{-1, 400},
			wantApplied: []string{"d1"},
		},
		{
			name:        "pourcentage exact au centime (-7 % de 10,00 € = 9,30 €)",
			orderType:   "IN",
			products:    []unitSpec{{"A", 1000, 1}},
			discounts:   []*models.DBDiscount{pct("d1", 7)},
			wantPrices:  []int{930},
			wantApplied: []string{"d1"},
		},
		{
			name:      "mode de commande incompatible",
			orderType: "TAKE_AWAY",
			products:  []unitSpec{{"A", 1000, 1}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 20)
				d.DiscountOrderType = "IN"
				return d
			}()},
			wantPrices:  []int{-1},
			wantApplied: []string{},
		},
		{
			name:      "mode vide (NULL en base) = tous les modes",
			orderType: "TAKE_AWAY",
			products:  []unitSpec{{"A", 1000, 1}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 20)
				d.DiscountOrderType = ""
				return d
			}()},
			wantPrices:  []int{800},
			wantApplied: []string{"d1"},
		},
		{
			name:      "plusieurs modes cochés dans le back-office",
			orderType: "TAKE_AWAY",
			products:  []unitSpec{{"A", 1000, 1}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 20)
				d.DiscountOrderType = "IN TAKE_AWAY"
				return d
			}()},
			wantPrices:  []int{800},
			wantApplied: []string{"d1"},
		},
		{
			name:      "code promo non saisi : pas de remise",
			orderType: "IN",
			products:  []unitSpec{{"A", 1000, 1}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 20)
				d.DiscountCode = strPtr("BIENVENUE")
				return d
			}()},
			wantPrices:  []int{-1},
			wantApplied: []string{},
		},
		{
			name:      "code promo erroné : pas de remise",
			orderType: "IN",
			code:      "AUTRE",
			products:  []unitSpec{{"A", 1000, 1}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 20)
				d.DiscountCode = strPtr("BIENVENUE")
				return d
			}()},
			wantPrices:  []int{-1},
			wantApplied: []string{},
		},
		{
			name:      "code promo correct",
			orderType: "IN",
			code:      "BIENVENUE",
			products:  []unitSpec{{"A", 1000, 1}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 20)
				d.DiscountCode = strPtr("BIENVENUE")
				return d
			}()},
			wantPrices:  []int{800},
			wantApplied: []string{"d1"},
		},
		{
			name:      "minimum QUANTITY (back-office) non atteint",
			orderType: "IN",
			products:  []unitSpec{{"A", 1000, 2}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 20)
				d.MinOrderUnit, d.MinOrderValue = "QUANTITY", 3
				return d
			}()},
			wantPrices:  []int{-1, -1},
			wantApplied: []string{},
		},
		{
			name:      "minimum QUANTITY (back-office) atteint",
			orderType: "IN",
			products:  []unitSpec{{"A", 1000, 3}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 20)
				d.MinOrderUnit, d.MinOrderValue = "QUANTITY", 3
				return d
			}()},
			wantPrices:  []int{800, -1, -1},
			wantApplied: []string{"d1"},
		},
		{
			name:      "minimum QTY (historique) non atteint",
			orderType: "IN",
			products:  []unitSpec{{"A", 1000, 2}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 20)
				d.MinOrderUnit, d.MinOrderValue = "QTY", 3
				return d
			}()},
			wantPrices:  []int{-1, -1},
			wantApplied: []string{},
		},
		{
			name:      "minimum EUR (back-office, centimes) non atteint",
			orderType: "IN",
			products:  []unitSpec{{"A", 1500, 1}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 20)
				d.MinOrderUnit, d.MinOrderValue = "EUR", 2000
				return d
			}()},
			wantPrices:  []int{-1},
			wantApplied: []string{},
		},
		{
			name:      "minimum EUR (back-office) atteint sur le total du panier",
			orderType: "IN",
			products:  []unitSpec{{"A", 1500, 1}, {"B", 500, 1}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 20)
				d.MinOrderUnit, d.MinOrderValue = "EUR", 2000
				return d
			}()},
			dp:          map[string]map[string]*models.DiscountProductInfo{"d1": targeted("B")},
			wantPrices:  []int{-1, 400},
			wantApplied: []string{"d1"},
		},
		{
			name:      "minimum CURRENCY (historique) non atteint",
			orderType: "IN",
			products:  []unitSpec{{"A", 1500, 1}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 20)
				d.MinOrderUnit, d.MinOrderValue = "CURRENCY", 2000
				return d
			}()},
			wantPrices:  []int{-1},
			wantApplied: []string{},
		},
		{
			name:      "unité de minimum inconnue ou vide : pas de condition",
			orderType: "IN",
			products:  []unitSpec{{"A", 1000, 1}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 20)
				d.MinOrderUnit, d.MinOrderValue = "", 99999
				return d
			}()},
			wantPrices:  []int{800},
			wantApplied: []string{"d1"},
		},
		{
			name:      "NEWPRICE avec prix saisi",
			orderType: "IN",
			products:  []unitSpec{{"A", 1000, 1}},
			discounts: []*models.DBDiscount{{DiscountID: "d1", DiscountUnit: "NEWPRICE", DiscountedQuantity: 1, Available: true}},
			dp: map[string]map[string]*models.DiscountProductInfo{"d1": {
				"A": {ProductID: "A", NewPrice: intPtr(650)},
			}},
			wantPrices:  []int{650},
			wantApplied: []string{"d1"},
		},
		{
			name:      "NEWPRICE sans prix saisi (NULL) : ni remise ni produit gratuit",
			orderType: "IN",
			products:  []unitSpec{{"A", 1000, 1}},
			discounts: []*models.DBDiscount{{DiscountID: "d1", DiscountUnit: "NEWPRICE", DiscountedQuantity: 1, Available: true}},
			dp: map[string]map[string]*models.DiscountProductInfo{"d1": {
				"A": {ProductID: "A", NewPrice: nil},
			}},
			wantPrices:  []int{-1},
			wantApplied: []string{},
		},
		{
			name:      "NEWPRICE sans prix ne bloque pas une promotion suivante",
			orderType: "IN",
			products:  []unitSpec{{"A", 1000, 1}},
			discounts: []*models.DBDiscount{
				{DiscountID: "d1", DiscountUnit: "NEWPRICE", DiscountedQuantity: 1, Available: true},
				pct("d2", 10),
			},
			dp: map[string]map[string]*models.DiscountProductInfo{"d1": {
				"A": {ProductID: "A", NewPrice: nil},
			}},
			wantPrices:  []int{900},
			wantApplied: []string{"d2"},
		},
		{
			name:        "montant fixe supérieur au prix : prix ramené à 0",
			orderType:   "IN",
			products:    []unitSpec{{"A", 200, 1}},
			discounts:   []*models.DBDiscount{{DiscountID: "d1", DiscountUnit: "CURRENCY", DiscountValue: 300, DiscountedQuantity: 1, Available: true}},
			wantPrices:  []int{0},
			wantApplied: []string{"d1"},
		},
		{
			name:      "plafond en montant",
			orderType: "IN",
			products:  []unitSpec{{"A", 1000, 1}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 50)
				d.MaxDiscountValue = f64Ptr(100)
				return d
			}()},
			wantPrices:  []int{900},
			wantApplied: []string{"d1"},
		},
		{
			name:      "plafond en pourcentage",
			orderType: "IN",
			products:  []unitSpec{{"A", 1000, 1}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 50)
				d.MaxDiscountValue, d.MaxDiscountUnit = f64Ptr(7), strPtr("PERCENTAGE")
				return d
			}()},
			wantPrices:  []int{930},
			wantApplied: []string{"d1"},
		},
		{
			name:      "cumulable par paliers de 2 : 4 unités remisées sur 5",
			orderType: "IN",
			products:  []unitSpec{{"A", 1000, 5}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 50)
				d.IsCumulative, d.DiscountedQuantity = true, 2
				return d
			}()},
			wantPrices:  []int{500, 500, 500, 500, -1},
			wantApplied: []string{"d1", "d1", "d1", "d1"},
		},
		{
			name:      "discounted_quantity 0 cumulable : pas de panique, pas de remise",
			orderType: "IN",
			products:  []unitSpec{{"A", 1000, 2}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 50)
				d.IsCumulative, d.DiscountedQuantity = true, 0
				return d
			}()},
			wantPrices:  []int{-1, -1},
			wantApplied: []string{},
		},
		{
			name:      "non cumulable sur 2 unités (discounted_quantity 2)",
			orderType: "IN",
			products:  []unitSpec{{"A", 1000, 3}},
			discounts: []*models.DBDiscount{func() *models.DBDiscount {
				d := pct("d1", 10)
				d.DiscountedQuantity = 2
				return d
			}()},
			wantPrices:  []int{900, 900, -1},
			wantApplied: []string{"d1", "d1"},
		},
		{
			name:        "une promotion non cumulable appliquée bloque les suivantes (ordre de priorité)",
			orderType:   "IN",
			products:    []unitSpec{{"A", 1000, 2}},
			discounts:   []*models.DBDiscount{pct("d1", 10), pct("d2", 50)},
			wantPrices:  []int{900, -1},
			wantApplied: []string{"d1"},
		},
		{
			name:      "promotions cumulables sur des produits différents",
			orderType: "IN",
			products:  []unitSpec{{"A", 1000, 1}, {"B", 500, 1}},
			discounts: []*models.DBDiscount{
				func() *models.DBDiscount { d := pct("d1", 10); d.IsCumulative = true; return d }(),
				func() *models.DBDiscount { d := pct("d2", 20); d.IsCumulative = true; return d }(),
			},
			dp: map[string]map[string]*models.DiscountProductInfo{
				"d1": targeted("A"),
				"d2": targeted("B"),
			},
			wantPrices:  []int{900, 400},
			wantApplied: []string{"d1", "d2"},
		},
		{
			name:      "un produit n'est jamais remisé deux fois",
			orderType: "IN",
			products:  []unitSpec{{"A", 1000, 1}},
			discounts: []*models.DBDiscount{
				func() *models.DBDiscount { d := pct("d1", 10); d.IsCumulative = true; return d }(),
				func() *models.DBDiscount { d := pct("d2", 20); d.IsCumulative = true; return d }(),
			},
			wantPrices:  []int{900},
			wantApplied: []string{"d1"},
		},
	}

	svc := &OrdersService{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			products, baseTotal := units(tc.products...)
			req := &models.PricingRequest{
				DiscountCode: tc.code,
				Order:        &models.OrderRequest{OrderType: tc.orderType},
			}
			dp := tc.dp
			if dp == nil {
				dp = map[string]map[string]*models.DiscountProductInfo{}
			}

			applied := svc.applyDiscounts(req, products, tc.discounts, dp, map[string]map[string][]models.DiscountOptionInfo{}, baseTotal)

			if got := unitPrices(products); !reflect.DeepEqual(got, tc.wantPrices) {
				t.Fatalf("prix remisés = %v, want %v", got, tc.wantPrices)
			}
			if !reflect.DeepEqual(applied, tc.wantApplied) {
				t.Fatalf("remises appliquées = %v, want %v", applied, tc.wantApplied)
			}
		})
	}
}

func TestNormalizeMinOrderUnit(t *testing.T) {
	cases := map[string]string{
		"QUANTITY": minOrderUnitQuantity,
		"QTY":      minOrderUnitQuantity,
		" qty ":    minOrderUnitQuantity,
		"EUR":      minOrderUnitCurrency,
		"CURRENCY": minOrderUnitCurrency,
		"":         "",
		"AUTRE":    "",
	}
	for in, want := range cases {
		if got := normalizeMinOrderUnit(in); got != want {
			t.Fatalf("normalizeMinOrderUnit(%q) = %q, want %q", in, got, want)
		}
	}
}

var discountColumns = []string{
	"discount_id", "discount_order_type", "discount_code", "discount_name", "discount_desc",
	"discount_value", "discount_unit", "min_order_value", "min_order_unit", "max_discount_value",
	"max_discount_unit", "discounted_quantity", "is_cumulative", "available", "prefered_order",
}

func TestGetDiscounts_QueryAndScan(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewOrdersRepository(db, nil)

	mock.ExpectQuery(`COALESCE\(d\.discount_order_type, ''\)`).
		WithArgs("42", "2026-09-21", "2026-09-21", "12:30:00", "12:30:00", 1).
		WillReturnRows(sqlmock.NewRows(discountColumns).
			// Promotion "tous modes" : COALESCE en SQL renvoie ''.
			AddRow("d1", "", nil, "Midi", "desc", 20, "PERCENTAGE", 1500.0, "EUR", nil, nil, 1, false, true, 0).
			// Même promotion renvoyée deux fois (deux créneaux correspondants).
			AddRow("d1", "", nil, "Midi", "desc", 20, "PERCENTAGE", 1500.0, "EUR", nil, nil, 1, false, true, 0).
			AddRow("d2", "IN", "CODE", "Code", "desc", 5, "CURRENCY", 0.0, "", 100.0, "CURRENCY", 1, true, true, 1))

	discounts, err := repo.GetDiscounts(context.Background(), &models.PricingRequest{
		MerchantID: "42",
		Time:       "2026-09-21 12:30:00",
		DayOfWeek:  1,
	})
	if err != nil {
		t.Fatalf("GetDiscounts: %v", err)
	}
	if len(discounts) != 2 || discounts[0].DiscountID != "d1" || discounts[1].DiscountID != "d2" {
		t.Fatalf("GetDiscounts must dedupe by discount_id and keep priority order, got %+v", discounts)
	}
	if discounts[0].MinOrderValue != 1500 || discounts[0].MinOrderUnit != "EUR" || discounts[0].DiscountOrderType != "" {
		t.Fatalf("unexpected scan of d1: %+v", discounts[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestGetDiscounts_SQLFilters(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(_, actual string) error {
		sql := strings.Join(strings.Fields(actual), " ")
		for _, fragment := range []string{
			"COALESCE(d.discount_order_type, '')",
			"COALESCE(d.min_order_unit, '')",
			"ds.enabled = TRUE",
			"d.enabled = TRUE",
			"d.available = TRUE",
			// Créneau [début, fin[ au jour et à l'heure locaux.
			// (fin à 00:00 = jusqu'à minuit).
			"ds.available_from <= ? AND (ds.available_to > ? OR ds.available_to = '00:00:00') AND ds.day_of_week = ?",
			// Dates de validité en dates calendaires locales, fin incluse.
			dbx.UTCDate("d.valid_from") + " <= ?",
			"(d.valid_to IS NULL OR " + dbx.UTCDate("d.valid_to") + " >= ?)",
			"ORDER BY d.prefered_order ASC, d.discount_id ASC",
		} {
			if !strings.Contains(sql, fragment) {
				return &missingFragmentError{fragment: fragment}
			}
		}
		if strings.Contains(sql, "AT TIME ZONE 'UTC' AS time") || strings.Contains(sql, "UTC_TIMESTAMP()") || strings.Contains(sql, "now()") {
			return &missingFragmentError{fragment: "no comparison to the database clock"}
		}
		return nil
	})))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewOrdersRepository(db, nil)

	mock.ExpectQuery("").WillReturnRows(sqlmock.NewRows(discountColumns))

	if _, err := repo.GetDiscounts(context.Background(), &models.PricingRequest{MerchantID: "42", Time: "2026-09-21 12:30:00", DayOfWeek: 1}); err != nil {
		t.Fatalf("GetDiscounts: %v", err)
	}
}

type missingFragmentError struct{ fragment string }

func (e *missingFragmentError) Error() string { return "SQL fragment missing: " + e.fragment }

// TestComputePricing_WithDiscounts calcule un panier complet (sur place) de
// bout en bout : 2 x A à 10,00 € + 1 x B à 5,00 €, promotion « tous modes »
// -20 % ciblant B avec un minimum de 10,00 € saisi en EUR, et une promotion
// NEWPRICE sur A sans prix saisi (ne doit rien changer).
func TestComputePricing_WithDiscounts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	svc := &OrdersService{ordersRepo: NewOrdersRepository(db, nil)}

	mock.ExpectQuery(`FROM merchant m`).
		WillReturnRows(sqlmock.NewRows([]string{"timezone", "currency", "delivery_fees", "delivery_fees_limit", "minimum_cart_for_delivery_order"}).
			AddRow("Europe/Paris", "EUR", 300, 2000, 0))
	mock.ExpectQuery(`WHERE status NOT IN`).
		WillReturnRows(sqlmock.NewRows([]string{"product_id", "name", "status"}))
	mock.ExpectQuery(`FROM products p\s+INNER JOIN tva_categories`).
		WillReturnRows(sqlmock.NewRows([]string{"product_id", "name", "price", "price_take_away", "price_delivery", "tva_rate_in", "tva_rate_delivery", "tva_rate_take_away"}).
			AddRow("A", "Burger", 1000, 1000, 1100, 10.0, 5.5, 5.5).
			AddRow("B", "Frites", 500, 500, 550, 10.0, 5.5, 5.5))
	mock.ExpectQuery(`COALESCE\(d\.discount_order_type, ''\)`).
		WillReturnRows(sqlmock.NewRows(discountColumns).
			AddRow("newprice-a", "", nil, "Prix spécial", "desc", 0, "NEWPRICE", 0.0, "", nil, nil, 1, false, true, 0).
			AddRow("frites", "", nil, "Frites -20%", "desc", 20, "PERCENTAGE", 1000.0, "EUR", nil, nil, 1, false, true, 1))
	mock.ExpectQuery(`FROM discounts_products dp`).
		WillReturnRows(sqlmock.NewRows([]string{"discount_id", "product_id", "new_price"}).
			AddRow("newprice-a", "A", nil).
			AddRow("frites", "B", nil))
	mock.ExpectQuery(`discounts_products_options`).
		WillReturnRows(sqlmock.NewRows([]string{"option_id", "product_id", "discount_id", "new_price", "is_option_mandatory"}))
	mock.ExpectQuery(`.+`).
		WillReturnRows(sqlmock.NewRows([]string{"seconds"}).AddRow(nil))

	req := &models.PricingRequest{
		MerchantID: "42",
		Order: &models.OrderRequest{
			OrderType: "IN",
			Products: []models.OrderProductPayload{
				{ProductID: "A", Quantity: 2},
				{ProductID: "B", Quantity: 1},
			},
		},
	}

	resp, err := svc.ComputePricing(context.Background(), req)
	if err != nil {
		t.Fatalf("ComputePricing: %v", err)
	}
	if resp.Status != "success" || len(resp.UnavailableProduct) != 0 {
		t.Fatalf("unexpected pricing response: %+v", resp)
	}

	order := resp.OrderRequest.Order
	if order.TTC != 2400 {
		t.Fatalf("TTC = %d, want 2400 (2 x 1000 + 500 - 20 %%)", order.TTC)
	}
	byProduct := map[string]models.OrderProductPayload{}
	for _, p := range order.Products {
		byProduct[p.ProductID] = p
	}
	if a := byProduct["A"]; a.Quantity != 2 || a.DiscountedPrice != nil {
		t.Fatalf("A must stay at full price (NEWPRICE without price), got %+v", a)
	}
	if b := byProduct["B"]; b.DiscountedPrice == nil || *b.DiscountedPrice != 400 || b.DiscountID == nil || *b.DiscountID != "frites" {
		t.Fatalf("B must be discounted to 400 by 'frites', got %+v", b)
	}
	if order.HT+order.TVA != order.TTC {
		t.Fatalf("HT (%d) + TVA (%d) != TTC (%d)", order.HT, order.TVA, order.TTC)
	}
	if _, err := time.Parse("2006-01-02 15:04:05", req.Time); err != nil {
		t.Fatalf("req.Time must be set in merchant local time, got %q", req.Time)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
