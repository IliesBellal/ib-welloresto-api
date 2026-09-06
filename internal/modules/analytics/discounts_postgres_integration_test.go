//go:build postgres_integration

package analytics

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestDiscounts_Postgres seeds a known, hand-computed dataset and checks
// GetDiscountsScopeTotals/GetDiscountsOrdersTotals/GetDiscountsPage/
// GetDiscountsMarginCoverage/GetDiscountsMeasurementCompleteFrom against it —
// PROMPT 22's mandatory accuracy test. Covers:
//   - the total discounted amount equals the exact sum of discount_redemptions
//     rows in scope (never a partial or double-counted sum);
//   - the répartition-par-remise breakdown sums exactly to that total, grouped
//     by discount_id — including a soft-deleted (enabled=false) discount,
//     which must still appear with its historical amount and IsDeleted=true;
//   - DiscountedOrdersCount matches COUNT(DISTINCT order_id) over exactly the
//     redemptions in scope;
//   - a canceled order's redemption and one outside the period are both
//     excluded, via the same AnalyticsOrdersScope join every other tab uses;
//   - the channel filter produces disjoint, summable subsets;
//   - deleting a redemption row (simulating PROMPT 21's "discount removed on
//     a reopened order") makes it disappear from every aggregate immediately
//     — this tab has no separate exclusion logic, it just reads
//     discount_redemptions as the live, corrected state;
//   - the reconstructed/measured split and MeasurementCompleteFrom;
//   - the margin-coverage query's partial-aggregation discipline (never a
//     complete revenue sum divided by a partial cost sum).
func TestDiscounts_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	var productID int64
	var discountD1, discountD2 int
	const merchantTZ = "Europe/Paris"

	cleanup := func() {
		if merchantIntID != 0 {
			mid := itoa(merchantIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM discount_redemptions WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM discounts_products WHERE discount_id_new IN (SELECT discount_id_new FROM discounts WHERE merchant_id = $1)`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM discounts WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
		if productID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, productID)
		}
	}
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Discounts Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-discounts', 'https://example.com', '0600000000', 'tok-discounts', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, price, category)
		VALUES ($1, 'ITest Discounts Product', 1000, 'itest-cat') RETURNING product_id`, merchantID).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	seedDiscount := func(name string, enabled bool) int {
		var id int
		if err := db.QueryRowContext(ctx, `
			INSERT INTO discounts (discount_id, merchant_id, discount_name, discount_desc, discount_value, discount_unit, valid_from, discounted_quantity, is_cumulative, is_time_limited, available, enabled)
			VALUES ('itest-disc-'||gen_random_uuid()::text, $1, $2, 'itest desc', 10, 'PERCENTAGE', now(), 1, false, false, true, $3)
			RETURNING discount_id_new`, merchantID, name, enabled).Scan(&id); err != nil {
			t.Fatalf("seed discount %s: %v", name, err)
		}
		return id
	}
	discountD1 = seedDiscount("ITest Promo Percent", true)
	discountD2 = seedDiscount("ITest Old Deal", false) // soft-deleted

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	seedOrder := func(orderNum int, brand, brandStatus, orderType string, priceCents int64, creationTime time.Time) int64 {
		var orderID int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, ht, tva, created_by, creation_date)
			VALUES ($1, $2, $3, $4, 'DONE', $5, $6, 0, 0, 'itest-discounts', $7)
			RETURNING order_id`,
			merchantID, orderNum, brand, brandStatus, orderType, priceCents, creationTime.UTC(),
		).Scan(&orderID); err != nil {
			t.Fatalf("seed order %d: %v", orderNum, err)
		}
		return orderID
	}

	seedOrderItem := func(orderID int64, priceCents int64, costPriceUnit *int64) int64 {
		var orderItemID int64
		var err error
		if costPriceUnit != nil {
			err = db.QueryRowContext(ctx, `
				INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price, cost_price_unit)
				VALUES ($1, $2, $3, 1, $4, $4, $5)
				RETURNING order_item_id`, orderID, productID, merchantID, priceCents, *costPriceUnit).Scan(&orderItemID)
		} else {
			err = db.QueryRowContext(ctx, `
				INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price)
				VALUES ($1, $2, $3, 1, $4, $4)
				RETURNING order_item_id`, orderID, productID, merchantID, priceCents).Scan(&orderItemID)
		}
		if err != nil {
			t.Fatalf("seed orderitem for order %d: %v", orderID, err)
		}
		return orderItemID
	}

	seedRedemption := func(discountID int, orderID, orderItemID int64, amountCents int64, isReconstructed bool, createdAt time.Time) {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO discount_redemptions (scope, discount_id, order_id, order_item_id, merchant_id, amount_applied_cents, is_reconstructed, created_at)
			VALUES ('PRODUCT_LINE', $1, $2, $3, $4, $5, $6, $7)`,
			discountID, orderID, orderItemID, merchantID, amountCents, isReconstructed, createdAt.UTC(),
		); err != nil {
			t.Fatalf("seed redemption (order %d): %v", orderID, err)
		}
	}

	// Window: 2026-01-01 .. 2026-02-01 (local).
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, loc).UTC()
	periodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, loc).UTC()

	cost500 := int64(500)

	// O1 — dine_in, in period, discount D1, reconstructed, cost known (covered).
	o1 := seedOrder(301, "WELLO_RESTO", "ACCEPTED", "IN", 800, time.Date(2026, 1, 5, 12, 0, 0, 0, loc))
	oi1 := seedOrderItem(o1, 800, &cost500)
	seedRedemption(discountD1, o1, oi1, 200, true, time.Date(2025, 1, 1, 0, 0, 0, 0, loc))

	// O2 — dine_in, in period, discount D1, measured (live), cost unknown.
	o2 := seedOrder(302, "WELLO_RESTO", "ACCEPTED", "IN", 900, time.Date(2026, 1, 10, 12, 0, 0, 0, loc))
	oi2 := seedOrderItem(o2, 900, nil)
	seedRedemption(discountD1, o2, oi2, 150, false, time.Date(2026, 1, 10, 0, 0, 0, 0, loc))

	// O3 — dine_in, in period, discount D2 (soft-deleted), measured.
	o3 := seedOrder(303, "WELLO_RESTO", "ACCEPTED", "IN", 1000, time.Date(2026, 1, 12, 12, 0, 0, 0, loc))
	oi3 := seedOrderItem(o3, 1000, nil)
	seedRedemption(discountD2, o3, oi3, 300, false, time.Date(2026, 1, 12, 0, 0, 0, 0, loc))

	// O4 — dine_in, in period, no discount at all.
	seedOrder(304, "WELLO_RESTO", "ACCEPTED", "IN", 1000, time.Date(2026, 1, 15, 12, 0, 0, 0, loc))

	// O5 — dine_in, in period, but CANCELED — its redemption must be excluded.
	o5 := seedOrder(305, "WELLO_RESTO", "CANCELED", "IN", 1234, time.Date(2026, 1, 8, 12, 0, 0, 0, loc))
	oi5 := seedOrderItem(o5, 1234, nil)
	seedRedemption(discountD1, o5, oi5, 999, false, time.Date(2026, 1, 8, 0, 0, 0, 0, loc))

	// O6 — dine_in, BEFORE the period — its redemption must be excluded.
	o6 := seedOrder(306, "WELLO_RESTO", "ACCEPTED", "IN", 1234, time.Date(2025, 12, 1, 12, 0, 0, 0, loc))
	oi6 := seedOrderItem(o6, 1234, nil)
	seedRedemption(discountD1, o6, oi6, 888, false, time.Date(2025, 12, 1, 0, 0, 0, 0, loc))

	// O7 — Uber Eats delivery, in period, discount D1, measured, for
	// channel-filter isolation.
	o7 := seedOrder(307, "UBER_EATS", "ACCEPTED", "DELIVERY", 1200, time.Date(2026, 1, 20, 12, 0, 0, 0, loc))
	oi7 := seedOrderItem(o7, 1200, nil)
	seedRedemption(discountD1, o7, oi7, 400, false, time.Date(2026, 1, 8, 0, 0, 0, 0, loc))

	repo := NewRepository(db)
	allChannels, _ := ChannelFilter(nil)

	// ---- Scope totals: O1+O2+O3+O7 = 4 redemptions, 200+150+300+400=1050 ----
	totals, err := repo.GetDiscountsScopeTotals(ctx, []string{merchantID}, allChannels, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("GetDiscountsScopeTotals: %v", err)
	}
	if totals.TotalAmountCents != 1050 {
		t.Fatalf("expected total 1050, got %+v", totals)
	}
	if totals.ReconstructedAmountCents != 200 || totals.ReconstructedRedemptionsCount != 1 {
		t.Fatalf("expected reconstructed 200/1 (O1 only), got %+v", totals)
	}
	if totals.MeasuredAmountCents != 850 || totals.MeasuredRedemptionsCount != 3 {
		t.Fatalf("expected measured 850/3 (O2+O3+O7), got %+v", totals)
	}
	if totals.DiscountedOrdersCount != 4 {
		t.Fatalf("expected 4 distinct discounted orders (O1,O2,O3,O7), got %d", totals.DiscountedOrdersCount)
	}

	// ---- Orders totals: O1,O2,O3,O4,O7 = 5 orders (O5 canceled, O6 out of
	// period excluded), revenue = 800+900+1000+1000+1200 = 4900 ----
	ordersTotals, err := repo.GetDiscountsOrdersTotals(ctx, []string{merchantID}, allChannels, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("GetDiscountsOrdersTotals: %v", err)
	}
	if ordersTotals.TotalOrdersCount != 5 || ordersTotals.ReferenceRevenueTTCCents != 4900 {
		t.Fatalf("expected 5 orders / 4900 cents reference revenue, got %+v", ordersTotals)
	}

	// ---- Répartition par remise: D1 (O1+O2+O7) = 750/3, D2 (O3) = 300/1,
	// summing exactly to the 1050 total above. D2 must appear despite being
	// soft-deleted. ----
	rows, totalRows, err := repo.GetDiscountsPage(ctx, []string{merchantID}, allChannels, DiscountsSortAmount, "desc", 1, 50, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("GetDiscountsPage: %v", err)
	}
	if totalRows != 2 {
		t.Fatalf("expected 2 distinct discounts in the breakdown, got %d", totalRows)
	}
	var sumAmount, sumCount int64
	byID := make(map[int64]DiscountRow, len(rows))
	for _, row := range rows {
		byID[row.DiscountID] = row
		sumAmount += row.TotalAmountCents
		sumCount += row.RedemptionsCount
	}
	if sumAmount != 1050 || sumCount != 4 {
		t.Fatalf("expected breakdown to sum to 1050/4, got %d/%d (%+v)", sumAmount, sumCount, rows)
	}
	rowD1 := byID[int64(discountD1)]
	if rowD1.TotalAmountCents != 750 || rowD1.RedemptionsCount != 3 || rowD1.IsDeleted {
		t.Fatalf("expected D1 row 750/3, not deleted, got %+v", rowD1)
	}
	rowD2 := byID[int64(discountD2)]
	if rowD2.TotalAmountCents != 300 || rowD2.RedemptionsCount != 1 || !rowD2.IsDeleted {
		t.Fatalf("expected D2 row 300/1, deleted, got %+v", rowD2)
	}
	// Sorted DESC by amount by default: D1 (750) before D2 (300).
	if len(rows) != 2 || rows[0].DiscountID != int64(discountD1) || rows[1].DiscountID != int64(discountD2) {
		t.Fatalf("expected D1 then D2 (sorted desc by amount), got %+v", rows)
	}

	// ---- Channel filter: disjoint subsets summing to the unfiltered total ----
	dineInOnly, _ := ChannelFilter([]string{ChannelDineIn})
	ubereatsOnly, _ := ChannelFilter([]string{ChannelUberEatsDelivery})

	dineInTotals, err := repo.GetDiscountsScopeTotals(ctx, []string{merchantID}, dineInOnly, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("GetDiscountsScopeTotals (dine_in): %v", err)
	}
	ubereatsTotals, err := repo.GetDiscountsScopeTotals(ctx, []string{merchantID}, ubereatsOnly, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("GetDiscountsScopeTotals (ubereats_delivery): %v", err)
	}
	if dineInTotals.TotalAmountCents != 650 || dineInTotals.DiscountedOrdersCount != 3 {
		t.Fatalf("expected dine_in 650/3 (O1+O2+O3), got %+v", dineInTotals)
	}
	if ubereatsTotals.TotalAmountCents != 400 || ubereatsTotals.DiscountedOrdersCount != 1 {
		t.Fatalf("expected ubereats_delivery 400/1 (O7), got %+v", ubereatsTotals)
	}
	if dineInTotals.TotalAmountCents+ubereatsTotals.TotalAmountCents != totals.TotalAmountCents {
		t.Fatalf("expected channel-split totals to sum to the unfiltered total (%d), got %d+%d",
			totals.TotalAmountCents, dineInTotals.TotalAmountCents, ubereatsTotals.TotalAmountCents)
	}

	// ---- MeasurementCompleteFrom: NOT period-bound (a global fact about this
	// merchant, see its own doc comment) — the earliest is_reconstructed=false
	// row across ALL TIME is O6's (2025-12-01), even though O6 itself falls
	// outside [periodStart, periodEnd) and is correctly excluded from every
	// period-scoped total above. O1's reconstructed row (2025-01-01) must NOT
	// be considered despite being earlier still. ----
	earliest, ok, err := repo.GetDiscountsMeasurementCompleteFrom(ctx, []string{merchantID})
	if err != nil {
		t.Fatalf("GetDiscountsMeasurementCompleteFrom: %v", err)
	}
	if !ok || earliest.Format("2006-01-02") != "2025-12-01" {
		t.Fatalf("expected measurement complete from 2025-12-01 (O6, global, unbounded by period), got ok=%v value=%v", ok, earliest)
	}

	// ---- Margin coverage (PRODUCT_LINE scope, all channels): revenue total =
	// 800+900+1000+1200=3900, covered (cost known) = 800 (O1 only), discount
	// covered = 200 (O1's amount), cost covered = 500. Never a complete
	// revenue sum divided by a partial cost sum: both numerator and
	// denominator restricted to the same cost-known subset. ----
	margin, err := repo.GetDiscountsMarginCoverage(ctx, []string{merchantID}, allChannels, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("GetDiscountsMarginCoverage: %v", err)
	}
	if margin.RevenueTTCCentsTotal != 3900 {
		t.Fatalf("expected margin revenue total 3900, got %+v", margin)
	}
	if margin.RevenueTTCCentsCovered != 800 || margin.DiscountCentsCovered != 200 || margin.CostCentsCovered != 500 {
		t.Fatalf("expected covered subset 800/200/500 (O1 only), got %+v", margin)
	}

	// ---- The "discount removed from a reopened order" case (PROMPT 21):
	// deleting O2's redemption row must immediately drop it from every
	// aggregate — this tab has no separate exclusion logic, it just reads
	// discount_redemptions as the live, corrected state. ----
	if _, err := db.ExecContext(ctx, `DELETE FROM discount_redemptions WHERE order_id = $1`, o2); err != nil {
		t.Fatalf("simulate discount removal on O2: %v", err)
	}
	afterRemoval, err := repo.GetDiscountsScopeTotals(ctx, []string{merchantID}, allChannels, periodStart, periodEnd)
	if err != nil {
		t.Fatalf("GetDiscountsScopeTotals (after removal): %v", err)
	}
	if afterRemoval.TotalAmountCents != 900 || afterRemoval.DiscountedOrdersCount != 3 {
		t.Fatalf("expected 900/3 after O2's redemption is removed (1050-150, 4-1), got %+v", afterRemoval)
	}

	// ---- An unrecognized channel is rejected at the filter level ----
	if _, ok := ChannelFilter([]string{"mobile"}); ok {
		t.Fatalf("expected ChannelFilter to reject an unknown channel")
	}
}
