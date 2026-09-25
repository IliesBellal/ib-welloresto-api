//go:build postgres_integration

package analytics

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestOrderFilter_Postgres checks the source × order-type filter
// (order_filter.go) against a hand-computed dataset, on the queries of every
// tab that accepts it (CA, Commandes, Produits, Options, Annulations, Vente
// additionnelle). Beyond the totals asserted below, every filtered repository
// method is called once: applyOrderFilter appends `?` placeholders to a scope
// that each query then composes differently, so a call site that dropped the
// extra args would fail here with a bind-count error rather than in PROD.
func TestOrderFilter_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID, tvaID, productID int64
	const merchantTZ = "Europe/Paris"

	t.Cleanup(func() {
		if merchantIntID != 0 {
			m := itoa(merchantIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM upsell_suggestions WHERE merchant_id = $1`, m)
			_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, m)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, m)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
		if productID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, productID)
		}
		if tvaID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id = $1`, tvaID)
		}
	})

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest OrderFilter Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-orderfilter', 'https://example.com', '0600000000', 'tok-orderfilter', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchant := itoa(merchantIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('0', 'ITest OrderFilter TVA 20', 'itest', 20) RETURNING tva_id`).Scan(&tvaID); err != nil {
		t.Fatalf("seed tva_categories: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, category, price, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'ITest OrderFilter Product', 'itest-orderfilter-categ', 1000, $2, $2, $2) RETURNING product_id`,
		merchant, tvaID).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, loc)
	startUTC, endUTC := time.Date(2026, 3, 1, 0, 0, 0, 0, loc).UTC(), time.Date(2026, 3, 3, 0, 0, 0, 0, loc).UTC()
	current := PeriodWindow{Start: startUTC, End: endUTC}
	previous := PeriodWindow{Start: startUTC.AddDate(0, -1, 0), End: endUTC.AddDate(0, -1, 0)}
	previousYear := PeriodWindow{Start: startUTC.AddDate(-1, 0, 0), End: endUTC.AddDate(-1, 0, 0)}

	setSource := func(orderID int64, source interface{}) {
		if _, err := db.ExecContext(ctx, `UPDATE orders SET order_source = $1 WHERE order_id = $2`, source, orderID); err != nil {
			t.Fatalf("set order_source on %d: %v", orderID, err)
		}
	}
	seed := func(num int, brand, brandStatus, orderType string, price int64, createdBy string, source interface{}, cancelledBy *string, isUpsell bool) int64 {
		id := seedCancelOrder(t, ctx, db, merchant, num, brand, brandStatus, orderType, price, base, createdBy, cancelledBy, nil)
		setSource(id, source)
		seedUpsellOrderItem(t, ctx, db, id, productID, merchant, 1, price, isUpsell)
		return id
	}

	// Valid orders (sum 31000, 5 orders):
	seed(601, "WELLO_RESTO", "ACCEPTED", "IN", 1000, "itest-orderfilter-user", OrderSourcePOS, nil, true)
	seed(602, "WELLO_RESTO", "ACCEPTED", "TAKE_AWAY", 2000, "KIOSK", OrderSourceKiosk, nil, false)
	seed(603, "UBER_EATS", "ACCEPTED", "DELIVERY", 4000, "itest-orderfilter-uber", OrderSourceUberEats, nil, false)
	seed(604, "WELLO_RESTO", "ACCEPTED", "IN", 8000, "SCANNORDER", OrderSourceScanNOrder, nil, true)
	// order_source NULL: only visible while the source dimension is unrestricted.
	seed(605, "WELLO_RESTO", "ACCEPTED", "IN", 16000, "0", nil, nil, false)
	// Cancelled POS dine-in order: counts in the cancellation scopes only.
	seed(606, "WELLO_RESTO", "CANCELED", "IN", 500, "itest-orderfilter-user", OrderSourcePOS, strPtr("STAFF"), false)

	seedUpsellSuggestion(t, ctx, db, "itest-orderfilter-sugg-pos", merchant, base, true)
	seedUpsellSuggestion(t, ctx, db, "itest-orderfilter-sugg-kiosk", merchant, base, false)
	if _, err := db.ExecContext(ctx, `UPDATE upsell_suggestions SET channel = 'KIOSK' WHERE id = 'itest-orderfilter-sugg-kiosk'`); err != nil {
		t.Fatalf("set suggestion channel: %v", err)
	}

	merchants := []string{merchant}
	baseRepo := NewRepository(db)
	filtered := func(sources, orderTypes []string) *Repository {
		f, ok := NewOrderFilter(sources, orderTypes)
		if !ok {
			t.Fatalf("NewOrderFilter(%v, %v) rejected", sources, orderTypes)
		}
		return baseRepo.WithOrderFilter(f)
	}

	revenueCases := []struct {
		name       string
		repo       *Repository
		wantTTC    int64
		wantOrders int64
	}{
		{"no filter", baseRepo, 31000, 5},
		{"every source ticked keeps NULL-source rows", filtered(OrderSources, nil), 31000, 5},
		{"caisse + borne", filtered([]string{OrderSourcePOS, OrderSourceKiosk}, nil), 3000, 2},
		{"sur place (NULL source still included)", filtered(nil, []string{"IN"}), 25000, 3},
		{"scannorder sur place", filtered([]string{OrderSourceScanNOrder}, []string{"IN"}), 8000, 1},
		{"caisse à emporter", filtered([]string{OrderSourcePOS}, []string{"TAKE_AWAY"}), 0, 0},
	}
	for _, tc := range revenueCases {
		for _, includeHT := range []bool{false, true} {
			cur, _, _, err := tc.repo.GetRevenueTotalsThreePeriods(ctx, merchants, current, previous, previousYear, includeHT)
			if err != nil {
				t.Fatalf("%s: GetRevenueTotalsThreePeriods(includeHT=%v): %v", tc.name, includeHT, err)
			}
			if cur.TotalTTCCents != tc.wantTTC || cur.OrderCount != tc.wantOrders {
				t.Fatalf("%s (includeHT=%v): got TTC=%d orders=%d, want %d/%d", tc.name, includeHT, cur.TotalTTCCents, cur.OrderCount, tc.wantTTC, tc.wantOrders)
			}
		}
		orders, _, _, err := tc.repo.GetOrdersTotalsThreePeriods(ctx, merchants, current, previous, previousYear)
		if err != nil {
			t.Fatalf("%s: GetOrdersTotalsThreePeriods: %v", tc.name, err)
		}
		if orders.OrderCount != tc.wantOrders {
			t.Fatalf("%s: orders count %d, want %d", tc.name, orders.OrderCount, tc.wantOrders)
		}
		products, _, err := tc.repo.GetProductsScopeTotalsTwoPeriods(ctx, merchants, "", current, previous)
		if err != nil {
			t.Fatalf("%s: GetProductsScopeTotalsTwoPeriods: %v", tc.name, err)
		}
		if products.RevenueTTCCents != tc.wantTTC {
			t.Fatalf("%s: products revenue %d, want %d", tc.name, products.RevenueTTCCents, tc.wantTTC)
		}
		upsellOrders, _, err := tc.repo.GetUpsellOrdersTotalTwoPeriods(ctx, merchants, Channels, current, previous)
		if err != nil {
			t.Fatalf("%s: GetUpsellOrdersTotalTwoPeriods: %v", tc.name, err)
		}
		if upsellOrders != tc.wantOrders {
			t.Fatalf("%s: upsell denominator %d, want %d", tc.name, upsellOrders, tc.wantOrders)
		}
	}

	// Cancellations: POS-only sees the cancelled 606 and its denominator
	// (601 + 606); borne-only sees one created order and no cancellation.
	pos := filtered([]string{OrderSourcePOS}, nil)
	created, _, _, err := pos.GetOrdersCreatedCountThreePeriods(ctx, merchants, current, previous, previousYear)
	if err != nil {
		t.Fatalf("GetOrdersCreatedCountThreePeriods: %v", err)
	}
	cancelled, _, _, err := pos.GetCancellationsTotalsThreePeriods(ctx, merchants, current, previous, previousYear)
	if err != nil {
		t.Fatalf("GetCancellationsTotalsThreePeriods: %v", err)
	}
	if created != 2 || cancelled.CancelledCount != 1 || cancelled.CancelledAmountCents != 500 {
		t.Fatalf("POS cancellations: created=%d cancelled=%d amount=%d, want 2/1/500", created, cancelled.CancelledCount, cancelled.CancelledAmountCents)
	}
	kiosk := filtered([]string{OrderSourceKiosk}, nil)
	created, _, _, err = kiosk.GetOrdersCreatedCountThreePeriods(ctx, merchants, current, previous, previousYear)
	if err != nil {
		t.Fatalf("GetOrdersCreatedCountThreePeriods (kiosk): %v", err)
	}
	cancelled, _, _, err = kiosk.GetCancellationsTotalsThreePeriods(ctx, merchants, current, previous, previousYear)
	if err != nil {
		t.Fatalf("GetCancellationsTotalsThreePeriods (kiosk): %v", err)
	}
	if created != 1 || cancelled.CancelledCount != 0 {
		t.Fatalf("kiosk cancellations: created=%d cancelled=%d, want 1/0", created, cancelled.CancelledCount)
	}

	// Upsell lines: 601 (POS) and 604 (ScanNOrder) carry is_upsell.
	upsellCur, _, err := pos.GetUpsellTotalsWithOrdersTwoPeriods(ctx, merchants, Channels, current, previous)
	if err != nil {
		t.Fatalf("GetUpsellTotalsWithOrdersTwoPeriods: %v", err)
	}
	if upsellCur.UpsellLines != 1 || upsellCur.OrdersWithUpsellCount != 1 {
		t.Fatalf("POS upsell: lines=%d orders=%d, want 1/1", upsellCur.UpsellLines, upsellCur.OrdersWithUpsellCount)
	}

	// Suggestions follow the source dimension (POS/SNO/KIOSK), marketplaces have none.
	for _, tc := range []struct {
		name         string
		repo         *Repository
		wantProposed int64
	}{
		{"no filter", baseRepo, 2},
		{"borne", kiosk, 1},
		{"uber eats", filtered([]string{OrderSourceUberEats}, nil), 0},
		{"order type only", filtered(nil, []string{"DELIVERY"}), 2},
	} {
		proposed, _, err := tc.repo.GetUpsellSuggestionsTotals(ctx, merchants, startUTC, endUTC)
		if err != nil {
			t.Fatalf("%s: GetUpsellSuggestionsTotals: %v", tc.name, err)
		}
		if proposed != tc.wantProposed {
			t.Fatalf("%s: proposed %d, want %d", tc.name, proposed, tc.wantProposed)
		}
	}

	// Bind-count smoke test: every other filtered query must at least run.
	r := filtered([]string{OrderSourcePOS, OrderSourceScanNOrder}, []string{"IN"})
	productIDs := []string{itoa(productID)}
	allOptionTypes := []string{OptionTypePaid, OptionTypeFree, OptionTypeRemoved}
	smoke := map[string]func() error{
		"GetRevenueTotalsTTC": func() error { _, err := r.GetRevenueTotalsTTC(ctx, merchants, startUTC, endUTC); return err },
		"GetRevenueTotalsHT":  func() error { _, err := r.GetRevenueTotalsHT(ctx, merchants, startUTC, endUTC); return err },
		"GetRevenueTimeline":  func() error { _, err := r.GetRevenueTimeline(ctx, merchants, merchantTZ, startUTC, endUTC); return err },
		"GetRevenueByChannel": func() error { _, err := r.GetRevenueByChannel(ctx, merchants, startUTC, endUTC); return err },
		"GetRevenueByMerchant": func() error {
			_, err := r.GetRevenueByMerchant(ctx, merchants, startUTC, endUTC)
			return err
		},
		"GetOrdersTotals":    func() error { _, err := r.GetOrdersTotals(ctx, merchants, startUTC, endUTC); return err },
		"GetOrdersTimeline":  func() error { _, err := r.GetOrdersTimeline(ctx, merchants, merchantTZ, startUTC, endUTC); return err },
		"GetOrdersByChannel": func() error { _, err := r.GetOrdersByChannel(ctx, merchants, startUTC, endUTC); return err },
		"GetOrdersByMerchant": func() error {
			_, err := r.GetOrdersByMerchant(ctx, merchants, startUTC, endUTC)
			return err
		},
		"GetOrdersCreatedCount": func() error { _, err := r.GetOrdersCreatedCount(ctx, merchants, startUTC, endUTC); return err },
		"GetCancellationsTotals": func() error {
			_, err := r.GetCancellationsTotals(ctx, merchants, startUTC, endUTC)
			return err
		},
		"GetOrdersCreatedCountByMerchant": func() error {
			_, err := r.GetOrdersCreatedCountByMerchant(ctx, merchants, startUTC, endUTC)
			return err
		},
		"GetCancellationsTotalsByMerchant": func() error {
			_, err := r.GetCancellationsTotalsByMerchant(ctx, merchants, startUTC, endUTC)
			return err
		},
		"GetCancellationsByReason": func() error {
			_, err := r.GetCancellationsByReason(ctx, merchants, startUTC, endUTC)
			return err
		},
		"GetCancellationsByAuthorType": func() error {
			_, err := r.GetCancellationsByAuthorType(ctx, merchants, startUTC, endUTC)
			return err
		},
		"GetCancellationsByChannel": func() error {
			_, err := r.GetCancellationsByChannel(ctx, merchants, startUTC, endUTC)
			return err
		},
		"GetCancellationsByStaff": func() error {
			_, err := r.GetCancellationsByStaff(ctx, merchants, startUTC, endUTC)
			return err
		},
		"GetProductsScopeTotals": func() error {
			_, err := r.GetProductsScopeTotals(ctx, merchants, "itest-orderfilter-categ", startUTC, endUTC)
			return err
		},
		"GetProductsPage": func() error {
			_, _, err := r.GetProductsPage(ctx, merchants, "itest-orderfilter-categ", ProductsSortQuantity, "desc", 1, ProductsDefaultPageSize, startUTC, endUTC)
			return err
		},
		"GetProductsPreviousRevenue": func() error {
			_, err := r.GetProductsPreviousRevenue(ctx, merchants, productIDs, startUTC, endUTC)
			return err
		},
		"GetOptionsScopeTotals": func() error {
			_, err := r.GetOptionsScopeTotals(ctx, merchants, allOptionTypes, startUTC, endUTC)
			return err
		},
		"GetOptionsPage": func() error {
			_, _, err := r.GetOptionsPage(ctx, merchants, allOptionTypes, OptionsSortQuantity, "desc", 1, OptionsDefaultPageSize, startUTC, endUTC)
			return err
		},
		"GetOptionsProductTotals": func() error {
			_, err := r.GetOptionsProductTotals(ctx, merchants, productIDs, startUTC, endUTC)
			return err
		},
		"GetOptionsBasketShares": func() error {
			_, err := r.GetOptionsBasketShares(ctx, merchants, []string{"1"}, startUTC, endUTC)
			return err
		},
		"GetOptionsBasketSharesRemoved": func() error {
			_, err := r.GetOptionsBasketSharesRemoved(ctx, merchants, []string{"1"}, startUTC, endUTC)
			return err
		},
		"GetUpsellTotals": func() error { _, err := r.GetUpsellTotals(ctx, merchants, Channels, startUTC, endUTC); return err },
		"GetOrdersWithUpsellCount": func() error {
			_, err := r.GetOrdersWithUpsellCount(ctx, merchants, Channels, startUTC, endUTC)
			return err
		},
		"GetUpsellOrdersTotal": func() error {
			_, err := r.GetUpsellOrdersTotal(ctx, merchants, Channels, startUTC, endUTC)
			return err
		},
		"GetUpsellByStaff": func() error { _, err := r.GetUpsellByStaff(ctx, merchants, Channels, startUTC, endUTC); return err },
	}
	for name, run := range smoke {
		if err := run(); err != nil {
			t.Fatalf("%s with an active OrderFilter: %v", name, err)
		}
	}
}
