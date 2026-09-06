//go:build postgres_integration

package analytics

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestUpsell_Postgres seeds a known, hand-computed dataset and checks every
// GetUpsell*/GetUpsellByStaff/GetUpsellSuggestionsTotals/
// GetUpsellInstrumentationActive query against it — PROMPT 19's mandatory
// accuracy test. Covers:
//   - the migrated is_upsell/state/brand_status scope (carried over from
//     stats.StatsRepository verbatim — see upsell.go's doc comment) plus the
//     channel filter this package's tabs apply consistently;
//   - the rate's denominator (GetUpsellOrdersTotal, every order in
//     AnalyticsOrdersScope + channel filter, independent of is_upsell);
//   - by-staff excludes SCANNORDER/no-user orders from the ranking, the same
//     rule the by-staff numerator itself already applies;
//   - the InstrumentationActive toggle: true for an establishment with at
//     least one is_upsell=true row ever, false for one with none — PROMPT
//     19's central mechanism, literally exercised here (not just "mentally
//     inserted") rather than asserted by inspection alone;
//   - upsell_suggestions' proposed/accepted split, period-scoped, and its
//     independence from is_upsell/InstrumentationActive.
func TestUpsell_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantAIntID, merchantBIntID int64
	var tvaID20 int64
	var productID int64
	const merchantTZ = "Europe/Paris"

	cleanup := func() {
		if merchantAIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM upsell_suggestions WHERE merchant_id = $1`, itoa(merchantAIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, itoa(merchantAIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, itoa(merchantAIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantAIntID)
		}
		if merchantBIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, itoa(merchantBIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, itoa(merchantBIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantBIntID)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id LIKE 'itest-upsell-%'`)
		if productID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, productID)
		}
		if tvaID20 != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id = $1`, tvaID20)
		}
	}
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Upsell Merchant A', 'addr', '1', 'street', '75001', 'Paris', 'siret-upsell-a', 'https://example.com', '0600000000', 'tok-upsell-a', $1)
		RETURNING id`, merchantTZ).Scan(&merchantAIntID); err != nil {
		t.Fatalf("seed merchant A: %v", err)
	}
	merchantA := itoa(merchantAIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Upsell Merchant B (no instrumentation)', 'addr', '1', 'street', '75001', 'Paris', 'siret-upsell-b', 'https://example.com', '0600000000', 'tok-upsell-b', $1)
		RETURNING id`, merchantTZ).Scan(&merchantBIntID); err != nil {
		t.Fatalf("seed merchant B: %v", err)
	}
	merchantB := itoa(merchantBIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('0', 'ITest Upsell TVA 20', 'itest', 20) RETURNING tva_id`).Scan(&tvaID20); err != nil {
		t.Fatalf("seed tva_categories: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, category, price, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'ITest Upsell Product', 'itest-upsell-categ', 1000, $2, $2, $2) RETURNING product_id`,
		merchantA, tvaID20).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, name, first_name, last_name, password, email, token)
		VALUES ('itest-upsell-user1', 'Jean Test', 'Jean', 'Test', 'itest-pw', 'itest-upsell-user1@example.com', 'itest-upsell-user1-token')`); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, loc)
	startUTC, endUTC := time.Date(2026, 3, 1, 0, 0, 0, 0, loc).UTC(), time.Date(2026, 3, 3, 0, 0, 0, 0, loc).UTC()

	// A1: dine_in, real user, one upsell line (1000 TTC -> 833 HT @20%).
	orderA1 := seedCancelOrder(t, ctx, db, merchantA, 401, "WELLO_RESTO", "ACCEPTED", "IN", 1000, base, "itest-upsell-user1", nil, nil)
	seedUpsellOrderItem(t, ctx, db, orderA1, productID, merchantA, 1, 1000, true)
	// A2: takeaway, ScanNOrder self-service, one upsell line (2000 TTC -> 1667 HT).
	orderA2 := seedCancelOrder(t, ctx, db, merchantA, 402, "WELLO_RESTO", "ACCEPTED", "TAKE_AWAY", 2000, base, "SCANNORDER", nil, nil)
	seedUpsellOrderItem(t, ctx, db, orderA2, productID, merchantA, 1, 2000, true)
	// A3: dine_in, real user, NO upsell line — counts toward the denominator only.
	orderA3 := seedCancelOrder(t, ctx, db, merchantA, 403, "WELLO_RESTO", "ACCEPTED", "IN", 1500, base, "itest-upsell-user1", nil, nil)
	seedUpsellOrderItem(t, ctx, db, orderA3, productID, merchantA, 1, 1500, false)
	// A4: delivery, real user, one upsell line (3000 TTC -> 2500 HT) — excluded
	// when the channel filter is restricted to dine_in/takeaway.
	orderA4 := seedCancelOrder(t, ctx, db, merchantA, 404, "WELLO_RESTO", "ACCEPTED", "DELIVERY", 3000, base, "itest-upsell-user1", nil, nil)
	seedUpsellOrderItem(t, ctx, db, orderA4, productID, merchantA, 1, 3000, true)

	// Merchant B: a valid order with an orderitems row, but is_upsell is
	// always false — instrumentation must read as inactive.
	orderB1 := seedCancelOrder(t, ctx, db, merchantB, 501, "WELLO_RESTO", "ACCEPTED", "IN", 1000, base, "itest-upsell-user1", nil, nil)
	seedUpsellOrderItem(t, ctx, db, orderB1, productID, merchantB, 1, 1000, false)

	// upsell_suggestions: 3 proposed in-period on merchant A (1 accepted), 1
	// proposed OUTSIDE the period (must not be counted).
	seedUpsellSuggestion(t, ctx, db, "itest-upsell-sugg-1", merchantA, base, true)
	seedUpsellSuggestion(t, ctx, db, "itest-upsell-sugg-2", merchantA, base.Add(time.Hour), false)
	seedUpsellSuggestion(t, ctx, db, "itest-upsell-sugg-3", merchantA, base.Add(2*time.Hour), false)
	seedUpsellSuggestion(t, ctx, db, "itest-upsell-sugg-out-of-period", merchantA, base.AddDate(0, -1, 0), true)

	repo := NewRepository(db)

	dineInTakeaway := []string{ChannelDineIn, ChannelTakeaway}
	allChannels := append([]string(nil), Channels...)

	// --- Channel-filtered totals: only A1 (dine_in) + A2 (takeaway). ---
	totals, err := repo.GetUpsellTotals(ctx, []string{merchantA}, dineInTakeaway, startUTC, endUTC)
	if err != nil {
		t.Fatalf("GetUpsellTotals: %v", err)
	}
	if totals.UpsellLines != 2 {
		t.Fatalf("expected 2 upsell lines (A1,A2) for dine_in+takeaway, got %d", totals.UpsellLines)
	}
	if totals.UpsellRevenueHTCents != 833+1667 {
		t.Fatalf("expected upsell revenue HT 2500 (833+1667), got %d", totals.UpsellRevenueHTCents)
	}

	ordersWithUpsell, err := repo.GetOrdersWithUpsellCount(ctx, []string{merchantA}, dineInTakeaway, startUTC, endUTC)
	if err != nil {
		t.Fatalf("GetOrdersWithUpsellCount: %v", err)
	}
	if ordersWithUpsell != 2 {
		t.Fatalf("expected 2 orders with upsell (A1,A2), got %d", ordersWithUpsell)
	}

	totalOrders, err := repo.GetUpsellOrdersTotal(ctx, []string{merchantA}, dineInTakeaway, startUTC, endUTC)
	if err != nil {
		t.Fatalf("GetUpsellOrdersTotal: %v", err)
	}
	if totalOrders != 3 { // A1, A2, A3 (A4 is delivery, excluded by the filter)
		t.Fatalf("expected denominator 3 (A1,A2,A3), got %d", totalOrders)
	}

	// --- All-channels totals: A1+A2+A4 (A4 now included). ---
	totalsAll, err := repo.GetUpsellTotals(ctx, []string{merchantA}, allChannels, startUTC, endUTC)
	if err != nil {
		t.Fatalf("GetUpsellTotals (all channels): %v", err)
	}
	if totalsAll.UpsellLines != 3 {
		t.Fatalf("expected 3 upsell lines (A1,A2,A4) across all channels, got %d", totalsAll.UpsellLines)
	}
	if totalsAll.UpsellRevenueHTCents != 833+1667+2500 {
		t.Fatalf("expected upsell revenue HT 5000, got %d", totalsAll.UpsellRevenueHTCents)
	}

	// --- By-staff: SCANNORDER (A2) excluded; user1 credited with A1+A4 only
	// when the channel filter includes delivery. ---
	staffDineInTakeaway, err := repo.GetUpsellByStaff(ctx, []string{merchantA}, dineInTakeaway, startUTC, endUTC)
	if err != nil {
		t.Fatalf("GetUpsellByStaff (dine_in+takeaway): %v", err)
	}
	if len(staffDineInTakeaway) != 1 {
		t.Fatalf("expected exactly 1 staff row (SCANNORDER excluded), got %+v", staffDineInTakeaway)
	}
	if staffDineInTakeaway[0].UserID != "itest-upsell-user1" || staffDineInTakeaway[0].UpsellLines != 1 || staffDineInTakeaway[0].UpsellRevenueHTCents != 833 {
		t.Fatalf("expected user1 with 1 line / 833 HT (A1 only), got %+v", staffDineInTakeaway[0])
	}

	staffAll, err := repo.GetUpsellByStaff(ctx, []string{merchantA}, allChannels, startUTC, endUTC)
	if err != nil {
		t.Fatalf("GetUpsellByStaff (all channels): %v", err)
	}
	if len(staffAll) != 1 {
		t.Fatalf("expected exactly 1 staff row across all channels too, got %+v", staffAll)
	}
	if staffAll[0].UpsellLines != 2 || staffAll[0].UpsellRevenueHTCents != 833+2500 {
		t.Fatalf("expected user1 with 2 lines / 3333 HT (A1+A4, A2 still excluded), got %+v", staffAll[0])
	}

	// --- InstrumentationActive: true for A (has an is_upsell=true row, ever),
	// false for B (orderitems exist, but none ever carry is_upsell=true). ---
	activeA, err := repo.GetUpsellInstrumentationActive(ctx, []string{merchantA})
	if err != nil {
		t.Fatalf("GetUpsellInstrumentationActive (A): %v", err)
	}
	if !activeA {
		t.Fatalf("expected merchant A instrumentation active=true (A1/A2/A4 carry is_upsell=true)")
	}
	activeB, err := repo.GetUpsellInstrumentationActive(ctx, []string{merchantB})
	if err != nil {
		t.Fatalf("GetUpsellInstrumentationActive (B): %v", err)
	}
	if activeB {
		t.Fatalf("expected merchant B instrumentation active=false (B1 carries is_upsell=false only) — this is the exact toggle PROMPT 19 asks for")
	}

	// --- Suggestions: 3 proposed in-period (1 accepted), the 4th (older than
	// the period) excluded entirely. ---
	proposed, accepted, err := repo.GetUpsellSuggestionsTotals(ctx, []string{merchantA}, startUTC, endUTC)
	if err != nil {
		t.Fatalf("GetUpsellSuggestionsTotals: %v", err)
	}
	if proposed != 3 {
		t.Fatalf("expected 3 proposed suggestions in period, got %d", proposed)
	}
	if accepted != 1 {
		t.Fatalf("expected 1 accepted suggestion in period, got %d", accepted)
	}

	// --- Zero-order period: zeros, not an error. ---
	emptyStart, emptyEnd := time.Date(2020, 1, 1, 0, 0, 0, 0, loc).UTC(), time.Date(2020, 2, 1, 0, 0, 0, 0, loc).UTC()
	emptyTotals, err := repo.GetUpsellTotals(ctx, []string{merchantA}, allChannels, emptyStart, emptyEnd)
	if err != nil {
		t.Fatalf("GetUpsellTotals (empty period): %v", err)
	}
	if emptyTotals.UpsellLines != 0 || emptyTotals.UpsellRevenueHTCents != 0 {
		t.Fatalf("expected zero totals for a period with no orders, got %+v", emptyTotals)
	}
	emptyStaff, err := repo.GetUpsellByStaff(ctx, []string{merchantA}, allChannels, emptyStart, emptyEnd)
	if err != nil {
		t.Fatalf("GetUpsellByStaff (empty period): %v", err)
	}
	if len(emptyStaff) != 0 {
		t.Fatalf("expected no staff rows for a period with no orders, got %+v", emptyStaff)
	}
}

// seedUpsellOrderItem inserts one orderitems row with an explicit is_upsell
// value — the one column postgres_integration_test.go's seedOrderItem
// doesn't set (it always defaults false).
func seedUpsellOrderItem(t *testing.T, ctx context.Context, db *sql.DB, orderID, productID int64, merchantID string, quantity int, priceCents int64, isUpsell bool) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price, is_upsell)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		orderID, productID, merchantID, quantity, priceCents, isUpsell); err != nil {
		t.Fatalf("seed upsell orderitem for order %d: %v", orderID, err)
	}
}

// seedUpsellSuggestion inserts one upsell_suggestions row. accepted controls
// whether accepted_items is populated (upsell.Tracker.RecordAcceptance's
// real-world effect) — see GetUpsellSuggestionsTotals' doc comment.
func seedUpsellSuggestion(t *testing.T, ctx context.Context, db *sql.DB, id, merchantID string, createdAt time.Time, accepted bool) {
	t.Helper()
	var acceptedItems interface{}
	if accepted {
		acceptedItems = `[]`
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO upsell_suggestions (id, merchant_id, cart_signature, suggested_items, source, channel, accepted_items, created_at)
		VALUES ($1, $2, $3, '[]'::jsonb, 'itest', 'POS', $4::jsonb, $5)`,
		id, merchantID, "itest-cart-"+id, acceptedItems, createdAt.UTC()); err != nil {
		t.Fatalf("seed upsell_suggestions %s: %v", id, err)
	}
}
