//go:build postgres_integration

package analytics

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestOrdersPaymentsVAT_Postgres covers the Commandes/Règlements/TVA
// endpoints' accuracy against a hand-computed dataset, same spirit as
// TestRepository_Postgres for the CA tab. One shared seed, since all three
// endpoints read the same orders (plus payments/products/tva joins):
//   - covers present on some orders, absent on others (never a silent zero,
//     PROMPT 04 §1) ;
//   - a disabled payment (must be excluded from Règlements) ;
//   - a non-canonical mop value (must bucket to "other", never dropped) ;
//   - the canonical scope filters (lowercase brand_status, CANCELED state)
//     excluding two orders from every one of the three endpoints identically ;
//   - a midnight-boundary order for the timeline day-bucketing.
func TestOrdersPaymentsVAT_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	var tvaID20, tvaID10 int64
	var productDineIn, productDelivery int64
	const merchantTZ = "Europe/Paris"

	cleanup := func() {
		if merchantIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM payments WHERE merchant_id = $1`, itoa(merchantIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, itoa(merchantIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, itoa(merchantIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
		if productDineIn != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, productDineIn)
		}
		if productDelivery != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, productDelivery)
		}
		if tvaID20 != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id = $1`, tvaID20)
		}
		if tvaID10 != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id = $1`, tvaID10)
		}
	}
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest OPV Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-opv', 'https://example.com', '0600000000', 'tok-opv', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('0', 'ITest OPV TVA 20', 'itest', 20) RETURNING tva_id`).Scan(&tvaID20); err != nil {
		t.Fatalf("seed tva_categories 20: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('1', 'ITest OPV TVA 10', 'itest', 10) RETURNING tva_id`).Scan(&tvaID10); err != nil {
		t.Fatalf("seed tva_categories 10: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, category, price, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'ITest OPV Dine-in Product', 'itest-cat', 1200, $2, $2, $2) RETURNING product_id`,
		merchantID, tvaID20).Scan(&productDineIn); err != nil {
		t.Fatalf("seed dine-in product: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, category, price, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'ITest OPV Delivery Product', 'itest-cat', 2000, $2, $2, $2) RETURNING product_id`,
		merchantID, tvaID10).Scan(&productDelivery); err != nil {
		t.Fatalf("seed delivery product: %v", err)
	}

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	// Order A — dine-in, 4 covers, 2026-01-15 12:00 local, TTC 1200 @ 20%
	// -> HT 1000, VAT 200. Payment: CB, enabled.
	orderATime := time.Date(2026, 1, 15, 12, 0, 0, 0, loc)
	orderAID := seedOrderWithCovers(t, ctx, db, merchantID, 201, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1200, 4, orderATime)
	seedOrderItem(t, ctx, db, orderAID, productDineIn, merchantID, 1, 1200)
	seedPayment(t, ctx, db, merchantID, orderAID, "CB", 1200, true)

	// Order B — UberEats delivery, no covers, 2026-01-15 13:00 local, TTC 2000
	// @ 10% -> HT 1818, VAT 182. Payment: UBER_EATS, enabled.
	orderBTime := time.Date(2026, 1, 15, 13, 0, 0, 0, loc)
	orderBID := seedOrderWithCovers(t, ctx, db, merchantID, 202, "UBER_EATS", "placed", "CLOSED", "DELIVERY", 2000, 0, orderBTime)
	seedOrderItem(t, ctx, db, orderBID, productDelivery, merchantID, 1, 2000)
	seedPayment(t, ctx, db, merchantID, orderBID, "UBER_EATS", 2000, true)

	// Order C — takeaway, no covers, created 00:30 local on Jan 16 (winter,
	// UTC+1) -> must land in the Jan 16 timeline bucket. TTC 500 @ 20%
	// (dine-in product's tva_take_away_id) -> HT 417, VAT 83. Payment:
	// disabled — must be excluded from every Règlements total.
	orderCTime := time.Date(2026, 1, 16, 0, 30, 0, 0, loc)
	orderCID := seedOrderWithCovers(t, ctx, db, merchantID, 203, "WELLO_RESTO", "ACCEPTED", "DONE", "TAKE_AWAY", 500, 0, orderCTime)
	seedOrderItem(t, ctx, db, orderCID, productDineIn, merchantID, 1, 500)
	seedPayment(t, ctx, db, merchantID, orderCID, "CB", 500, false)

	// Order D — lowercase brand_status 'deleted': excluded from every
	// endpoint by the canonical scope, same as the CA tab.
	orderDTime := time.Date(2026, 1, 15, 14, 0, 0, 0, loc)
	orderDID := seedOrderWithCovers(t, ctx, db, merchantID, 204, "WELLO_RESTO", "deleted", "DONE", "IN", 9999, 8, orderDTime)
	seedOrderItem(t, ctx, db, orderDID, productDineIn, merchantID, 1, 9999)

	// Order E — state CANCELED: excluded from every endpoint.
	orderETime := time.Date(2026, 1, 15, 15, 0, 0, 0, loc)
	orderEID := seedOrderWithCovers(t, ctx, db, merchantID, 205, "WELLO_RESTO", "ACCEPTED", "CANCELED", "IN", 8888, 3, orderETime)
	seedOrderItem(t, ctx, db, orderEID, productDineIn, merchantID, 1, 8888)

	// Order F — dine-in, 2 covers, 2026-01-15 16:00 local, TTC 800 @ 20% ->
	// HT 667, VAT 133. Payment: a non-canonical mop value — must bucket to
	// "other", never silently dropped (AUDIT.md P14).
	orderFTime := time.Date(2026, 1, 15, 16, 0, 0, 0, loc)
	orderFID := seedOrderWithCovers(t, ctx, db, merchantID, 206, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 800, 2, orderFTime)
	seedOrderItem(t, ctx, db, orderFID, productDineIn, merchantID, 1, 800)
	seedPayment(t, ctx, db, merchantID, orderFID, "PERCENTAGE", 800, true)

	repo := NewRepository(db)
	periodStartUTC, periodEndUTC := time.Date(2026, 1, 15, 0, 0, 0, 0, loc).UTC(), time.Date(2026, 1, 17, 0, 0, 0, 0, loc).UTC()

	// ---- Commandes ----

	ordersTotals, err := repo.GetOrdersTotals(ctx, []string{merchantID}, periodStartUTC, periodEndUTC)
	if err != nil {
		t.Fatalf("GetOrdersTotals: %v", err)
	}
	if ordersTotals.OrderCount != 4 {
		t.Fatalf("expected 4 orders in scope (A,B,C,F), got %d", ordersTotals.OrderCount)
	}
	if ordersTotals.TotalTTCCents != 4500 {
		t.Fatalf("expected TTC total 4500, got %d", ordersTotals.TotalTTCCents)
	}
	if ordersTotals.OrdersWithCovers != 2 {
		t.Fatalf("expected 2 orders with covers (A,F), got %d", ordersTotals.OrdersWithCovers)
	}
	if ordersTotals.TotalCovers != 6 {
		t.Fatalf("expected 6 total covers (4+2), got %d", ordersTotals.TotalCovers)
	}
	if ordersTotals.TTCCentsOfOrdersWithCovers != 2000 {
		t.Fatalf("expected 2000 TTC among covers-bearing orders (1200+800), got %d", ordersTotals.TTCCentsOfOrdersWithCovers)
	}

	period := ordersPeriodTotals("2026-01-15", "2026-01-16", ordersTotals)
	if !period.CoversDataAvailable {
		t.Fatalf("expected CoversDataAvailable=true, got false")
	}
	if period.TotalCovers == nil || *period.TotalCovers != 6 {
		t.Fatalf("expected TotalCovers=6, got %+v", period.TotalCovers)
	}
	if period.AvgBasketPerCoverCents == nil || *period.AvgBasketPerCoverCents != 333 {
		t.Fatalf("expected AvgBasketPerCoverCents=333 (2000/6), got %+v", period.AvgBasketPerCoverCents)
	}
	if period.AvgBasketTTCCents != 1125 {
		t.Fatalf("expected AvgBasketTTCCents=1125 (4500/4), got %d", period.AvgBasketTTCCents)
	}

	// A period with no orders at all must report covers as unavailable, not
	// a silent zero.
	emptyTotals, err := repo.GetOrdersTotals(ctx, []string{merchantID}, time.Date(2020, 1, 1, 0, 0, 0, 0, loc).UTC(), time.Date(2020, 2, 1, 0, 0, 0, 0, loc).UTC())
	if err != nil {
		t.Fatalf("GetOrdersTotals (empty period): %v", err)
	}
	emptyPeriod := ordersPeriodTotals("2020-01-01", "2020-01-31", emptyTotals)
	if emptyPeriod.CoversDataAvailable {
		t.Fatalf("expected CoversDataAvailable=false for a zero-order period, got true")
	}
	if emptyPeriod.TotalCovers != nil || emptyPeriod.AvgBasketPerCoverCents != nil {
		t.Fatalf("expected nil covers fields for a zero-order period, got %+v / %+v", emptyPeriod.TotalCovers, emptyPeriod.AvgBasketPerCoverCents)
	}

	ordersByChannel, err := repo.GetOrdersByChannel(ctx, []string{merchantID}, periodStartUTC, periodEndUTC)
	if err != nil {
		t.Fatalf("GetOrdersByChannel: %v", err)
	}
	ordersChannelCounts := map[string]int64{}
	for _, c := range ordersByChannel {
		ordersChannelCounts[c.Channel] = c.OrderCount
	}
	if ordersChannelCounts[ChannelDineIn] != 2 {
		t.Fatalf("expected dine_in=2 (A,F), got %+v", ordersChannelCounts)
	}
	if ordersChannelCounts[ChannelTakeaway] != 1 {
		t.Fatalf("expected takeaway=1 (C), got %+v", ordersChannelCounts)
	}
	if ordersChannelCounts[ChannelUberEatsDelivery] != 1 {
		t.Fatalf("expected ubereats_delivery=1 (B), got %+v", ordersChannelCounts)
	}
	var ordersChannelCountSum int64
	for _, c := range ordersByChannel {
		ordersChannelCountSum += c.OrderCount
	}
	if ordersChannelCountSum != ordersTotals.OrderCount {
		t.Fatalf("orders by_channel parts sum to %d, want exactly the period total %d", ordersChannelCountSum, ordersTotals.OrderCount)
	}

	ordersTimeline, err := repo.GetOrdersTimeline(ctx, []string{merchantID}, merchantTZ, periodStartUTC, periodEndUTC)
	if err != nil {
		t.Fatalf("GetOrdersTimeline: %v", err)
	}
	ordersByDay := map[string]int64{}
	for _, p := range ordersTimeline {
		ordersByDay[p.LocalDay] = p.TotalOrders
	}
	if ordersByDay["2026-01-15"] != 3 {
		t.Fatalf("expected 3 orders on 2026-01-15 (A,B,F), got by-day=%+v", ordersByDay)
	}
	if ordersByDay["2026-01-16"] != 1 {
		t.Fatalf("expected 1 order on 2026-01-16 (C, created 00:30 local), got by-day=%+v", ordersByDay)
	}

	// ---- Règlements ----

	paymentsTotals, err := repo.GetPaymentsTotals(ctx, []string{merchantID}, periodStartUTC, periodEndUTC)
	if err != nil {
		t.Fatalf("GetPaymentsTotals: %v", err)
	}
	if paymentsTotals.TotalAmountCents != 4000 {
		t.Fatalf("expected payments total 4000 (1200+2000+800, excluding C's disabled payment), got %d", paymentsTotals.TotalAmountCents)
	}
	if paymentsTotals.PaymentCount != 3 {
		t.Fatalf("expected 3 payments (A,B,F), got %d", paymentsTotals.PaymentCount)
	}

	byMethod, err := repo.GetPaymentsByMethod(ctx, []string{merchantID}, periodStartUTC, periodEndUTC)
	if err != nil {
		t.Fatalf("GetPaymentsByMethod: %v", err)
	}
	methodAmounts := map[string]int64{}
	for _, m := range byMethod {
		methodAmounts[m.Method] = m.TotalAmountCents
	}
	if methodAmounts[PaymentMethodCB] != 1200 {
		t.Fatalf("expected CB=1200, got %+v", methodAmounts)
	}
	if methodAmounts[PaymentMethodUberEats] != 2000 {
		t.Fatalf("expected UBER_EATS=2000, got %+v", methodAmounts)
	}
	if methodAmounts[PaymentMethodOther] != 800 {
		t.Fatalf("expected other=800 (PERCENTAGE bucketed as other, never dropped), got %+v", methodAmounts)
	}
	var byMethodSum int64
	for _, m := range byMethod {
		byMethodSum += m.TotalAmountCents
	}
	if byMethodSum != paymentsTotals.TotalAmountCents {
		t.Fatalf("payments by_method parts sum to %d, want exactly the period total %d", byMethodSum, paymentsTotals.TotalAmountCents)
	}

	// ---- TVA ----

	vatTotals, err := repo.GetVATTotals(ctx, []string{merchantID}, periodStartUTC, periodEndUTC)
	if err != nil {
		t.Fatalf("GetVATTotals: %v", err)
	}
	if vatTotals.TotalTTCCents != 4500 {
		t.Fatalf("expected VAT-scope TTC 4500, got %d", vatTotals.TotalTTCCents)
	}
	wantHT := int64(1000 + 1818 + 417 + 667) // A+B+C+F
	if vatTotals.TotalHTCents != wantHT {
		t.Fatalf("expected HT total %d, got %d", wantHT, vatTotals.TotalHTCents)
	}

	byRateShares, err := repo.GetVATByRate(ctx, []string{merchantID}, periodStartUTC, periodEndUTC)
	if err != nil {
		t.Fatalf("GetVATByRate: %v", err)
	}
	byRate := apportionVATByRate(byRateShares, vatTotals.TotalHTCents)
	rateHT := map[float64]int64{}
	var rateHTSum, rateVATSum int64
	for _, rt := range byRate {
		rateHT[rt.Rate] = rt.BaseHTCents
		rateHTSum += rt.BaseHTCents
		rateVATSum += rt.VATCents
	}
	// Raw (unrounded) group sums: 20% group (A+C+F) = 1200*100/120 +
	// 500*100/120 + 800*100/120 = 1000 + 416.666... + 666.666... =
	// 2083.333...; 10% group (B) = 2000*100/110 = 1818.181.... Floored and
	// summed that's 2083+1818=3901, one cent short of the period total
	// (3902, itself independently rounded from the whole period's ungrouped
	// sum — see the wantHT assertion above). PROMPT 06 §1's fix hands that
	// leftover cent to the group with the largest fractional remainder —
	// 20%'s .333 beats 10%'s .181 — so 20% becomes 2084, not 2083. The point
	// isn't the specific number: it's that rateHTSum/rateVATSum always equal
	// the period totals exactly, unlike before this fix.
	if rateHTSum != vatTotals.TotalHTCents {
		t.Fatalf("by_rate HT parts sum to %d, want exactly the period total %d", rateHTSum, vatTotals.TotalHTCents)
	}
	if wantVAT := vatTotals.TotalTTCCents - vatTotals.TotalHTCents; rateVATSum != wantVAT {
		t.Fatalf("by_rate VAT parts sum to %d, want exactly %d", rateVATSum, wantVAT)
	}
	if rateHT[20] != 2084 {
		t.Fatalf("expected 20%% rate HT=2084 (A+C+F, largest remainder absorbs the leftover cent), got %+v", rateHT)
	}
	if rateHT[10] != 1818 {
		t.Fatalf("expected 10%% rate HT=1818 (B), got %+v", rateHT)
	}

	byChannelShares, err := repo.GetVATByChannel(ctx, []string{merchantID}, periodStartUTC, periodEndUTC)
	if err != nil {
		t.Fatalf("GetVATByChannel: %v", err)
	}
	byChannelVAT := apportionVATByChannel(byChannelShares, vatTotals.TotalHTCents)
	channelHT := map[string]int64{}
	var channelHTSum, channelVATSum int64
	for _, c := range byChannelVAT {
		channelHT[c.Channel] = c.BaseHTCents
		channelHTSum += c.BaseHTCents
		channelVATSum += c.VATCents
	}
	if channelHTSum != vatTotals.TotalHTCents {
		t.Fatalf("by_channel HT parts sum to %d, want exactly the period total %d", channelHTSum, vatTotals.TotalHTCents)
	}
	if wantVAT := vatTotals.TotalTTCCents - vatTotals.TotalHTCents; channelVATSum != wantVAT {
		t.Fatalf("by_channel VAT parts sum to %d, want exactly %d", channelVATSum, wantVAT)
	}
	if channelHT[ChannelDineIn] != 1000+667 {
		t.Fatalf("expected dine_in HT=%d (A+F), got %+v", 1000+667, channelHT)
	}
	if channelHT[ChannelTakeaway] != 417 {
		t.Fatalf("expected takeaway HT=417 (C), got %+v", channelHT)
	}
	if channelHT[ChannelUberEatsDelivery] != 1818 {
		t.Fatalf("expected ubereats_delivery HT=1818 (B), got %+v", channelHT)
	}
}

func seedOrderWithCovers(t *testing.T, ctx context.Context, db *sql.DB, merchantID string, orderNum int, brand, brandStatus, state, orderType string, priceCents int64, coversCount int, creationTime time.Time) int64 {
	t.Helper()
	var orderID int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, ht, tva, created_by, creation_date, places_settings)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0, 0, 'itest-analytics', $8, $9)
		RETURNING order_id`,
		merchantID, orderNum, brand, brandStatus, state, orderType, priceCents, creationTime.UTC(), coversCount,
	).Scan(&orderID)
	if err != nil {
		t.Fatalf("seed order with covers (%s/%s/%s): %v", brand, state, orderType, err)
	}
	return orderID
}

func seedPayment(t *testing.T, ctx context.Context, db *sql.DB, merchantID string, orderID int64, mop string, amountCents int64, enabled bool) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, enabled)
		VALUES ($1, 'itest-analytics-user', $2, $3, $4, $5)`,
		merchantID, orderID, amountCents, mop, enabled); err != nil {
		t.Fatalf("seed payment for order %d: %v", orderID, err)
	}
}
