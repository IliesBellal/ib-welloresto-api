//go:build postgres_integration

package analytics

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestRepository_Postgres seeds a known, hand-computed dataset and checks
// every GetRevenue* query against an independent calculation — PROMPT 03
// Partie 4's mandatory accuracy test. Covers:
//   - the canonical scope (state IN CLOSED/DONE, brand_status excluded
//     case-insensitively, all brands, no created_by filter)
//   - TTC totals from orders.price
//   - HT recomputed from orderitems×products×tva_categories (not orders.ht,
//     which is 0 for every marketplace order)
//   - channel derivation (brand × order_type)
//   - the local-day timeline, including an order created at 00:30 local time
//   - an establishment with zero orders in the requested period
func TestRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	var tvaID20, tvaID10 int64
	var productDineIn, productDelivery int64
	const merchantTZ = "Europe/Paris"

	cleanup := func() {
		if merchantIntID != 0 {
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
		VALUES ('ITest Analytics Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-analytics', 'https://example.com', '0600000000', 'tok-analytics', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('0', 'ITest TVA 20', 'itest', 20) RETURNING tva_id`).Scan(&tvaID20); err != nil {
		t.Fatalf("seed tva_categories 20: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('1', 'ITest TVA 10', 'itest', 10) RETURNING tva_id`).Scan(&tvaID10); err != nil {
		t.Fatalf("seed tva_categories 10: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, category, price, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'ITest Dine-in Product', 'itest-cat', 1200, $2, $2, $2) RETURNING product_id`,
		merchantID, tvaID20).Scan(&productDineIn); err != nil {
		t.Fatalf("seed dine-in product: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, category, price, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'ITest Delivery Product', 'itest-cat', 2000, $2, $2, $2) RETURNING product_id`,
		merchantID, tvaID10).Scan(&productDelivery); err != nil {
		t.Fatalf("seed delivery product: %v", err)
	}

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	// Order A — WELLO_RESTO dine-in, 2026-01-15 12:00 local, TTC 1200,
	// tva_rate 20 -> HT = 1200*100/120 = 1000 exactly.
	orderATime := time.Date(2026, 1, 15, 12, 0, 0, 0, loc)
	orderAID := seedOrder(t, ctx, db, merchantID, 101, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1200, orderATime)
	seedOrderItem(t, ctx, db, orderAID, productDineIn, merchantID, 1, 1200)

	// Order B — UBER_EATS delivery, 2026-01-15 13:00 local, orders.ht/tva
	// left at 0 (mirrors the real marketplace integrations), TTC 2000,
	// tva_rate 10 -> HT = 2000*100/110 = 1818.18... rounds to 1818.
	orderBTime := time.Date(2026, 1, 15, 13, 0, 0, 0, loc)
	orderBID := seedOrder(t, ctx, db, merchantID, 102, "UBER_EATS", "placed", "CLOSED", "DELIVERY", 2000, orderBTime)
	seedOrderItem(t, ctx, db, orderBID, productDelivery, merchantID, 1, 2000)

	// Order C — created at 00:30 local time on 2026-01-16 (winter, UTC+1 ->
	// 2026-01-15 23:30 UTC). Must be attributed to the Jan 16 local day, not
	// Jan 15 — PROMPT 03 Partie 4's explicit "commande créée à 00h30 heure
	// de Paris" check.
	orderCTime := time.Date(2026, 1, 16, 0, 30, 0, 0, loc)
	orderCID := seedOrder(t, ctx, db, merchantID, 103, "WELLO_RESTO", "ACCEPTED", "DONE", "TAKE_AWAY", 500, orderCTime)
	seedOrderItem(t, ctx, db, orderCID, productDineIn, merchantID, 1, 500)

	// Order D — same window, but brand_status is lowercase 'deleted': must
	// be excluded by upper(brand_status) NOT IN (...), not just a bare
	// NOT IN (PERIMETRE.md's 8-row PROD case).
	orderDTime := time.Date(2026, 1, 15, 14, 0, 0, 0, loc)
	orderDID := seedOrder(t, ctx, db, merchantID, 104, "WELLO_RESTO", "deleted", "DONE", "IN", 9999, orderDTime)
	seedOrderItem(t, ctx, db, orderDID, productDineIn, merchantID, 1, 9999)

	// Order E — same window, state CANCELED: must be excluded by the
	// state IN ('CLOSED','DONE') filter.
	orderETime := time.Date(2026, 1, 15, 15, 0, 0, 0, loc)
	orderEID := seedOrder(t, ctx, db, merchantID, 105, "WELLO_RESTO", "ACCEPTED", "CANCELED", "IN", 8888, orderETime)
	seedOrderItem(t, ctx, db, orderEID, productDineIn, merchantID, 1, 8888)

	repo := NewRepository(db)

	periodStartUTC, periodEndUTC := time.Date(2026, 1, 15, 0, 0, 0, 0, loc).UTC(), time.Date(2026, 1, 17, 0, 0, 0, 0, loc).UTC()

	// --- TTC totals: only A, B, C count (1200 + 2000 + 500 = 3700). D and E
	// must be excluded. ---
	totals, err := repo.GetRevenueTotalsTTC(ctx, []string{merchantID}, periodStartUTC, periodEndUTC)
	if err != nil {
		t.Fatalf("GetRevenueTotalsTTC: %v", err)
	}
	if totals.TotalTTCCents != 3700 {
		t.Fatalf("expected TTC total 3700 (excluding lowercase-deleted and canceled orders), got %d", totals.TotalTTCCents)
	}
	if totals.OrderCount != 3 {
		t.Fatalf("expected 3 orders in scope, got %d", totals.OrderCount)
	}

	// --- HT recompute: 1000 (A) + 1818 (B) + independent calc for C
	// (500 * 100/120 = 416.66... rounds to 417) = 3235. ---
	htCents, err := repo.GetRevenueTotalsHT(ctx, []string{merchantID}, periodStartUTC, periodEndUTC)
	if err != nil {
		t.Fatalf("GetRevenueTotalsHT: %v", err)
	}
	wantHT := int64(1000 + 1818 + 417)
	if htCents != wantHT {
		t.Fatalf("expected HT total %d, got %d", wantHT, htCents)
	}

	// --- Channel breakdown: dine_in=1200 (A), ubereats_delivery=2000 (B),
	// takeaway=500 (C). ---
	byChannel, err := repo.GetRevenueByChannel(ctx, []string{merchantID}, periodStartUTC, periodEndUTC)
	if err != nil {
		t.Fatalf("GetRevenueByChannel: %v", err)
	}
	channelTotals := map[string]int64{}
	for _, c := range byChannel {
		channelTotals[c.Channel] = c.TotalTTCCents
	}
	if channelTotals[ChannelDineIn] != 1200 {
		t.Fatalf("expected dine_in=1200, got %+v", channelTotals)
	}
	if channelTotals[ChannelUberEatsDelivery] != 2000 {
		t.Fatalf("expected ubereats_delivery=2000, got %+v", channelTotals)
	}
	if channelTotals[ChannelTakeaway] != 500 {
		t.Fatalf("expected takeaway=500, got %+v", channelTotals)
	}
	// PROMPT 06 §1: whatever ventilation is shown next to a total, its parts
	// must sum to exactly that total. Revenue's by_channel never needed the
	// apportion.go fix (TTC is an exact integer SUM, no per-group rounding
	// involved) — this guards the invariant against a future regression, not
	// just this fix.
	var channelTTCSum int64
	for _, c := range byChannel {
		channelTTCSum += c.TotalTTCCents
	}
	if channelTTCSum != totals.TotalTTCCents {
		t.Fatalf("by_channel TTC parts sum to %d, want exactly the period total %d", channelTTCSum, totals.TotalTTCCents)
	}

	// --- Timeline: order C (created 00:30 on Jan 16 local) must land in the
	// 2026-01-16 bucket, not 2026-01-15 — the midnight-boundary check. ---
	timeline, err := repo.GetRevenueTimeline(ctx, []string{merchantID}, merchantTZ, periodStartUTC, periodEndUTC)
	if err != nil {
		t.Fatalf("GetRevenueTimeline: %v", err)
	}
	byDay := map[string]*RevenueDayPoint{}
	for i := range timeline {
		byDay[timeline[i].LocalDay] = &timeline[i]
	}
	day15 := byDay["2026-01-15"]
	day16 := byDay["2026-01-16"]
	if day15 == nil || day15.TotalTTCCents != 3200 {
		t.Fatalf("expected 2026-01-15 total 3200 (orders A+B), got %+v", day15)
	}
	if day16 == nil || day16.TotalTTCCents != 500 {
		t.Fatalf("expected 2026-01-16 total 500 (order C, created 00:30 local), got %+v", day16)
	}

	// --- DST spring-forward: an order at 2026-03-30 00:30 local (30 minutes
	// after France springs forward on 2026-03-29 02:00->03:00) is
	// 2026-03-29 22:30 UTC. A per-row DST-aware conversion buckets it under
	// 2026-03-30; a single offset computed from the period's start date
	// (fixed +01:00, pre-transition) would misbucket it under 2026-03-29 —
	// this is the bug GetRevenueTimeline's tzName parameter (not tzOffset)
	// fixes. ---
	dstOrderTime := time.Date(2026, 3, 30, 0, 30, 0, 0, loc)
	dstOrderID := seedOrder(t, ctx, db, merchantID, 106, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 700, dstOrderTime)
	seedOrderItem(t, ctx, db, dstOrderID, productDineIn, merchantID, 1, 700)

	dstPeriodStartUTC, dstPeriodEndUTC := time.Date(2026, 3, 28, 0, 0, 0, 0, loc).UTC(), time.Date(2026, 3, 31, 0, 0, 0, 0, loc).UTC()
	dstTimeline, err := repo.GetRevenueTimeline(ctx, []string{merchantID}, merchantTZ, dstPeriodStartUTC, dstPeriodEndUTC)
	if err != nil {
		t.Fatalf("GetRevenueTimeline (DST period): %v", err)
	}
	dstByDay := map[string]int64{}
	for _, p := range dstTimeline {
		dstByDay[p.LocalDay] = p.TotalTTCCents
	}
	if dstByDay["2026-03-30"] != 700 {
		t.Fatalf("expected 2026-03-30 total 700 (DST spring-forward order), got by-day=%+v", dstByDay)
	}
	if dstByDay["2026-03-29"] != 0 {
		t.Fatalf("DST spring-forward order leaked into 2026-03-29, got by-day=%+v", dstByDay)
	}

	// --- Zero-order establishment: a period with no seeded orders must
	// return zero, not an error. ---
	emptyStart, emptyEnd := time.Date(2020, 1, 1, 0, 0, 0, 0, loc).UTC(), time.Date(2020, 2, 1, 0, 0, 0, 0, loc).UTC()
	emptyTotals, err := repo.GetRevenueTotalsTTC(ctx, []string{merchantID}, emptyStart, emptyEnd)
	if err != nil {
		t.Fatalf("GetRevenueTotalsTTC (empty period): %v", err)
	}
	if emptyTotals.TotalTTCCents != 0 || emptyTotals.OrderCount != 0 {
		t.Fatalf("expected zero totals for a period with no orders, got %+v", emptyTotals)
	}
}

func seedOrder(t *testing.T, ctx context.Context, db *sql.DB, merchantID string, orderNum int, brand, brandStatus, state, orderType string, priceCents int64, creationTime time.Time) int64 {
	t.Helper()
	var orderID int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, ht, tva, created_by, creation_date)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0, 0, 'itest-analytics', $8)
		RETURNING order_id`,
		merchantID, orderNum, brand, brandStatus, state, orderType, priceCents, creationTime.UTC(),
	).Scan(&orderID)
	if err != nil {
		t.Fatalf("seed order (%s/%s/%s): %v", brand, state, orderType, err)
	}
	return orderID
}

func seedOrderItem(t *testing.T, ctx context.Context, db *sql.DB, orderID, productID int64, merchantID string, quantity int, priceCents int64) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price)
		VALUES ($1, $2, $3, $4, $5)`, orderID, productID, merchantID, quantity, priceCents); err != nil {
		t.Fatalf("seed orderitem for order %d: %v", orderID, err)
	}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
