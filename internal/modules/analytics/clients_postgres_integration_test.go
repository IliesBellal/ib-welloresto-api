//go:build postgres_integration

package analytics

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestClients_Postgres seeds a known, hand-computed dataset and checks
// GetCustomersCoverage/GetCustomersLifetimeStats and the segmentation they
// feed (computeClientsSegments) against it — PROMPT 18's mandatory accuracy
// test. Covers the three calculation traps named in PROMPT 18 §3:
//   - "nouveau" is whether the customer's FIRST ORDER EVER (not bounded to
//     the period) falls in the window — a customer whose only order predates
//     the period must never be classified nouveau just because a naive query
//     restricted MIN(creation_date) to the period;
//   - lifetime value/lifetime orders are summed across the customer's WHOLE
//     history, not just the period, even when most of that history predates
//     the window;
//   - customer.customer_nb_orders/customer_total_spent/last_order_date are
//     never read — this test seeds them with deliberately WRONG values to
//     prove the repository query ignores them entirely and still gets the
//     right answer from `orders`.
//
// Also covers: a canceled order excluded from every aggregate, an
// unidentified (customer_id NULL) order counted in coverage but never in the
// per-customer stats, the channel filter producing disjoint, summable
// subsets, and the full nouveau/récurrent/fidèle/inactif/dormant partition.
func TestClients_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	var productID int64
	var custA, custB, custC, custD, custE int64
	const merchantTZ = "Europe/Paris"

	cleanup := func() {
		if merchantIntID != 0 {
			mid := itoa(merchantIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM customer WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
		if productID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, productID)
		}
	}
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Clients Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-clients', 'https://example.com', '0600000000', 'tok-clients', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, price, category)
		VALUES ($1, 'ITest Clients Product', 1000, 'itest-cat') RETURNING product_id`, merchantID).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	// seedCustomer deliberately writes WRONG customer_nb_orders/
	// customer_total_spent/last_order_date — proving GetCustomersLifetimeStats
	// never reads them (PROMPT 18 §3).
	seedCustomer := func(firstName, lastName string) int64 {
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO customer (merchant_id, customer_first_name, customer_last_name, customer_nb_orders, customer_total_spent, last_order_date, creation_date, enabled)
			VALUES ($1, $2, $3, 999, 999999, $4, $4, TRUE) RETURNING customer_id`,
			merchantID, firstName, lastName, time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC),
		).Scan(&id); err != nil {
			t.Fatalf("seed customer %s %s: %v", firstName, lastName, err)
		}
		return id
	}

	seedOrderWithCustomer := func(orderNum int, brand, brandStatus, orderType string, priceCents int64, customerID int64, creationTime time.Time) int64 {
		var orderID int64
		err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, ht, tva, created_by, creation_date, customer_id)
			VALUES ($1, $2, $3, $4, 'DONE', $5, $6, 0, 0, 'itest-clients', $7, $8)
			RETURNING order_id`,
			merchantID, orderNum, brand, brandStatus, orderType, priceCents, creationTime.UTC(), customerID,
		).Scan(&orderID)
		if err != nil {
			t.Fatalf("seed order (customer %d): %v", customerID, err)
		}
		return orderID
	}
	seedOrderNoCustomer := func(orderNum int, priceCents int64, creationTime time.Time) int64 {
		var orderID int64
		err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, ht, tva, created_by, creation_date)
			VALUES ($1, $2, 'WELLO_RESTO', 'ACCEPTED', 'DONE', 'IN', $3, 0, 0, 'itest-clients', $4)
			RETURNING order_id`,
			merchantID, orderNum, priceCents, creationTime.UTC(),
		).Scan(&orderID)
		if err != nil {
			t.Fatalf("seed order without customer: %v", err)
		}
		return orderID
	}

	// Window: 2026-01-01 .. 2026-02-01 (local), a short (31-day) window so the
	// dormant bucket (only reachable when the window is shorter than
	// clientsInactivityDays=180) can actually be exercised.
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, loc).UTC()
	periodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, loc).UTC()

	custA = seedCustomer("ITestA", "Nouveau")
	custB = seedCustomer("ITestB", "Recurrent")
	custC = seedCustomer("ITestC", "Fidele")
	custD = seedCustomer("ITestD", "Inactif")
	custE = seedCustomer("ITestE", "Dormant")
	custF := seedCustomer("ITestF", "UberEats") // channel-filter isolation

	// A — nouveau: first (and only) order EVER falls inside the window.
	seedOrderWithCustomer(201, "WELLO_RESTO", "ACCEPTED", "IN", 1000, custA, time.Date(2026, 1, 10, 12, 0, 0, 0, loc))

	// B — récurrent: first order well before the window, second order inside
	// it. Lifetime orders = 2 (< clientsLoyalOrdersThreshold), so récurrent,
	// not fidèle. Lifetime value must sum BOTH orders (1200+1500=2700), never
	// just the period's own 1500.
	seedOrderWithCustomer(202, "WELLO_RESTO", "ACCEPTED", "IN", 1200, custB, time.Date(2024, 6, 1, 12, 0, 0, 0, loc))
	seedOrderWithCustomer(203, "WELLO_RESTO", "ACCEPTED", "IN", 1500, custB, time.Date(2026, 1, 15, 12, 0, 0, 0, loc))

	// C — fidèle: 5 lifetime orders, last one inside the window.
	for i, num := range []int{204, 205, 206, 207} {
		seedOrderWithCustomer(num, "WELLO_RESTO", "ACCEPTED", "IN", 1000, custC, time.Date(2024, 1, i+1, 12, 0, 0, 0, loc))
	}
	seedOrderWithCustomer(208, "WELLO_RESTO", "ACCEPTED", "IN", 2000, custC, time.Date(2026, 1, 20, 12, 0, 0, 0, loc))

	// D — inactif: only order is 2024-01-01, far more than 180 days before
	// periodEnd (2026-02-01) and before periodStart.
	seedOrderWithCustomer(209, "WELLO_RESTO", "ACCEPTED", "IN", 900, custD, time.Date(2024, 1, 1, 12, 0, 0, 0, loc))

	// E — dormant: last order before periodStart, but less than 180 days
	// before periodEnd (2026-02-01 - 180d = 2025-08-05) — 2025-10-01 qualifies:
	// not active this period, not yet stale enough to be inactif.
	seedOrderWithCustomer(210, "WELLO_RESTO", "ACCEPTED", "IN", 800, custE, time.Date(2025, 10, 1, 12, 0, 0, 0, loc))

	// F — Uber Eats delivery order inside the window, for channel-filter
	// isolation (disjoint from the WELLO_RESTO dine-in orders above).
	seedOrderWithCustomer(211, "UBER_EATS", "ACCEPTED", "DELIVERY", 2500, custF, time.Date(2026, 1, 12, 12, 0, 0, 0, loc))

	// A canceled order for a brand-new customer — must be excluded entirely
	// from every aggregate (never counted as a "new customer" or contribute
	// to lifetime value), same AnalyticsOrdersScope every other tab uses.
	custCanceled := seedCustomer("ITestCanceled", "Excluded")
	seedOrderWithCustomer(212, "WELLO_RESTO", "CANCELED", "IN", 5000, custCanceled, time.Date(2026, 1, 5, 12, 0, 0, 0, loc))

	// An order with no identified customer at all — must count toward
	// coverage's TotalOrders but never appear in GetCustomersLifetimeStats.
	seedOrderNoCustomer(213, 1100, time.Date(2026, 1, 8, 12, 0, 0, 0, loc))

	repo := NewRepository(db)
	allChannels, _ := ChannelFilter(nil)

	// ---- Coverage: 6 orders in scope (A,B,B,C,C,C,C,C,D,E,F = 10 covered) +
	// 1 uncovered = 11 total in [periodStart, periodEnd). The canceled order
	// (212) is excluded from AnalyticsOrdersScope entirely, so it counts in
	// neither bucket. ----
	coverage, err := repo.GetCustomersCoverage(ctx, []string{merchantID}, allChannels, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("GetCustomersCoverage: %v", err)
	}
	// In-window orders: A(201), B(203), C(208), F(211) = 4 with customer_id,
	// plus the 1 uncovered order (213) = 5 total. (B's 2024 order and C's
	// 2024 orders and D/E's orders are all before periodStart, so they don't
	// count toward THIS PERIOD's coverage at all — coverage is period-scoped,
	// unlike the lifetime stats.)
	if coverage.OrdersWithCustomer != 4 || coverage.TotalOrders != 5 {
		t.Fatalf("expected coverage 4/5, got %+v", coverage)
	}

	// ---- Lifetime stats, all channels ----
	rows, err := repo.GetCustomersLifetimeStats(ctx, []string{merchantID}, allChannels, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("GetCustomersLifetimeStats: %v", err)
	}
	byID := make(map[string]CustomerLifetimeRow, len(rows))
	for _, r := range rows {
		byID[r.CustomerID] = r
	}
	if len(rows) != 6 {
		t.Fatalf("expected 6 identified customers (A-F), got %d: %+v", len(rows), rows)
	}
	if _, ok := byID[itoa(custCanceled)]; ok {
		t.Fatalf("a customer whose only order was canceled must not appear at all")
	}

	rowA := byID[itoa(custA)]
	if rowA.LifetimeOrders != 1 || rowA.LifetimeValueCents != 1000 || rowA.PeriodOrders != 1 || rowA.PeriodRevenueCents != 1000 {
		t.Fatalf("customer A: expected lifetime=1/1000, period=1/1000, got %+v", rowA)
	}

	rowB := byID[itoa(custB)]
	// Lifetime value MUST include the 2024 order (1200) even though it
	// predates the window — this is the "valeur vie cumulée depuis toujours"
	// check (PROMPT 18 §3/Vérification).
	if rowB.LifetimeOrders != 2 || rowB.LifetimeValueCents != 2700 {
		t.Fatalf("customer B: expected lifetime orders=2 value=2700 (1200+1500, summed across ALL time), got %+v", rowB)
	}
	if rowB.PeriodOrders != 1 || rowB.PeriodRevenueCents != 1500 {
		t.Fatalf("customer B: expected period orders=1 revenue=1500 (only the in-window order), got %+v", rowB)
	}

	rowC := byID[itoa(custC)]
	if rowC.LifetimeOrders != 5 || rowC.LifetimeValueCents != 6000 { // 4*1000 + 2000
		t.Fatalf("customer C: expected lifetime orders=5 value=6000, got %+v", rowC)
	}

	rowD := byID[itoa(custD)]
	if rowD.LifetimeOrders != 1 || rowD.PeriodOrders != 0 {
		t.Fatalf("customer D: expected lifetime=1 period=0, got %+v", rowD)
	}

	rowE := byID[itoa(custE)]
	if rowE.LifetimeOrders != 1 || rowE.PeriodOrders != 0 {
		t.Fatalf("customer E: expected lifetime=1 period=0, got %+v", rowE)
	}

	// ---- The "nouveau" trap: restricting first-order to the period would
	// make B/C/D/E all look new (their MIN(creation_date) WITHIN the period
	// only exists for B and C, and would be wrongly read as their first
	// order). The real FirstOrderDate must reflect their true earliest order,
	// which predates the window for all of B/C/D/E. ----
	if !rowB.FirstOrderDate.Before(periodStart) {
		t.Fatalf("customer B: expected first_order_date before the window (2024-06-01), got %v", rowB.FirstOrderDate)
	}
	if !rowC.FirstOrderDate.Before(periodStart) {
		t.Fatalf("customer C: expected first_order_date before the window (2024-01-01), got %v", rowC.FirstOrderDate)
	}

	// ---- Segmentation: exactly the expected bucket per customer ----
	buckets := computeClientsSegments(rows, periodStart, periodEnd)
	assertIn := func(seg string, customerID int64) {
		t.Helper()
		for _, r := range buckets[seg] {
			if r.CustomerID == itoa(customerID) {
				return
			}
		}
		t.Fatalf("expected customer %d in segment %q, buckets=%+v", customerID, seg, buckets)
	}
	assertIn(ClientsSegmentNew, custA)
	assertIn(ClientsSegmentReturning, custB)
	assertIn(ClientsSegmentLoyal, custC)
	assertIn(ClientsSegmentInactive, custD)
	assertIn(ClientsSegmentDormant, custE)
	// custF (Uber Eats) is also a first-time-ever order in the window -> nouveau.
	assertIn(ClientsSegmentNew, custF)

	// ---- Channel filter: disjoint subsets, summing to the unfiltered total ----
	dineInOnly, _ := ChannelFilter([]string{ChannelDineIn})
	deliveryOnly, _ := ChannelFilter([]string{ChannelUberEatsDelivery})

	dineInRows, err := repo.GetCustomersLifetimeStats(ctx, []string{merchantID}, dineInOnly, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("GetCustomersLifetimeStats (dine_in): %v", err)
	}
	ubereatsRows, err := repo.GetCustomersLifetimeStats(ctx, []string{merchantID}, deliveryOnly, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("GetCustomersLifetimeStats (ubereats_delivery): %v", err)
	}
	if len(dineInRows)+len(ubereatsRows) != len(rows) {
		t.Fatalf("expected dine_in (%d) + ubereats_delivery (%d) to partition the unfiltered set (%d), got mismatch", len(dineInRows), len(ubereatsRows), len(rows))
	}
	if len(ubereatsRows) != 1 || ubereatsRows[0].CustomerID != itoa(custF) {
		t.Fatalf("expected exactly customer F under ubereats_delivery, got %+v", ubereatsRows)
	}
	for _, r := range dineInRows {
		if r.CustomerID == itoa(custF) {
			t.Fatalf("customer F (Uber Eats only) must not appear under the dine_in filter")
		}
	}

	dineInCoverage, err := repo.GetCustomersCoverage(ctx, []string{merchantID}, dineInOnly, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("GetCustomersCoverage (dine_in): %v", err)
	}
	ubereatsCoverage, err := repo.GetCustomersCoverage(ctx, []string{merchantID}, deliveryOnly, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("GetCustomersCoverage (ubereats_delivery): %v", err)
	}
	// Every in-window order is either WELLO_RESTO dine-in or Uber Eats
	// delivery in this fixture, so the two coverage totals must sum exactly
	// to the unfiltered total (5) — no order counted twice, none dropped.
	if dineInCoverage.TotalOrders+ubereatsCoverage.TotalOrders != coverage.TotalOrders {
		t.Fatalf("expected channel-split totals (%d+%d) to equal unfiltered total (%d)", dineInCoverage.TotalOrders, ubereatsCoverage.TotalOrders, coverage.TotalOrders)
	}

	// ---- An establishment/period with zero identified customers: empty
	// slice, no error, not a fabricated result. ----
	emptyStart, emptyEnd := time.Date(2020, 1, 1, 0, 0, 0, 0, loc).UTC(), time.Date(2020, 2, 1, 0, 0, 0, 0, loc).UTC()
	emptyRows, err := repo.GetCustomersLifetimeStats(ctx, []string{merchantID}, allChannels, emptyStart, emptyEnd)
	if err != nil {
		t.Fatalf("GetCustomersLifetimeStats (empty period): %v", err)
	}
	if len(emptyRows) != 0 {
		t.Fatalf("expected zero rows for a period long before any seeded order, got %d", len(emptyRows))
	}
	emptyCoverage, err := repo.GetCustomersCoverage(ctx, []string{merchantID}, allChannels, emptyStart, emptyEnd)
	if err != nil {
		t.Fatalf("GetCustomersCoverage (empty period): %v", err)
	}
	if emptyCoverage.TotalOrders != 0 || emptyCoverage.OrdersWithCustomer != 0 {
		t.Fatalf("expected zero coverage for a period with no orders, got %+v", emptyCoverage)
	}

	// ---- An unrecognized channel is rejected at the filter level, not
	// silently ignored (service.go's job, but belt and suspenders). ----
	if _, ok := ChannelFilter([]string{"mobile"}); ok {
		t.Fatalf("expected ChannelFilter to reject an unknown channel")
	}
}
