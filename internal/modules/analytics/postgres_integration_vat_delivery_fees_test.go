//go:build postgres_integration

package analytics

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestVATDeliveryFees_Postgres is the PROMPT 09 lot 3 (C5) regression test:
// GetVATTotals/GetVATByRate/GetVATByChannel must now include delivery fee
// VAT (orders.delivery_fees), via the tva_id=-1 UNION ALL branch — see
// repository.go's deliveryFeeHTExpr/deliveryFeeJoins doc comment. Isolated
// from TestOrdersPaymentsVAT_Postgres's shared fixture (that test's orders
// all default delivery_fees=0, so this doesn't overlap it) because tva_id=-1
// is a real, fixed referential row (not auto-generated) that must be seeded
// and torn down explicitly.
func TestVATDeliveryFees_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	var tvaID20 int64
	var productID int64
	const merchantTZ = "Europe/Paris"
	seededDeliveryFeeCategory := false

	cleanup := func() {
		if merchantIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, itoa(merchantIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, itoa(merchantIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
		if productID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, productID)
		}
		if tvaID20 != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id = $1`, tvaID20)
		}
		if seededDeliveryFeeCategory {
			_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id = -1`)
		}
	}
	t.Cleanup(cleanup)

	// tva_id=-1 is a fixed, referential id (not IDENTITY-generated) — only
	// seed it if this Postgres doesn't already carry it, and only clean up
	// what this test added.
	var existing int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tva_categories WHERE tva_id = -1`).Scan(&existing); err != nil {
		t.Fatalf("check existing tva_id=-1: %v", err)
	}
	if existing == 0 {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO tva_categories (tva_id, delivery_type, tva_title, tva_desc, tva_rate, enabled, show_in_report)
			VALUES (-1, '2', 'ITest Delivery Fees', 'itest', 20, false, false)`); err != nil {
			t.Fatalf("seed tva_id=-1: %v", err)
		}
		seededDeliveryFeeCategory = true
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest VAT Delivery Fees', 'addr', '1', 'street', '75001', 'Paris', 'siret-vdf', 'https://example.com', '0600000000', 'tok-vdf', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('0', 'ITest VDF TVA 20', 'itest', 20) RETURNING tva_id`).Scan(&tvaID20); err != nil {
		t.Fatalf("seed tva_categories 20: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, category, price, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'ITest VDF Delivery Product', 'itest-cat', 1000, $2, $2, $2) RETURNING product_id`,
		merchantID, tvaID20).Scan(&productID); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	orderTime := time.Date(2026, 2, 10, 12, 0, 0, 0, loc)

	// Order A — WELLO_RESTO delivery, product line TTC 1000 @ 20% (HT 833,
	// VAT 167) PLUS delivery_fees=500 @ tva_id=-1's 20% (HT 417, VAT 83).
	orderAID := seedOrderWithDeliveryFees(t, ctx, db, merchantID, 301, "WELLO_RESTO", "ACCEPTED", "CLOSED", "DELIVERY", 1000, 500, orderTime)
	seedOrderItem(t, ctx, db, orderAID, productID, merchantID, 1, 1000)

	repo := NewRepository(db)
	periodStartUTC := time.Date(2026, 2, 10, 0, 0, 0, 0, loc).UTC()
	periodEndUTC := time.Date(2026, 2, 11, 0, 0, 0, 0, loc).UTC()

	totals, err := repo.GetVATTotals(ctx, []string{merchantID}, periodStartUTC, periodEndUTC)
	if err != nil {
		t.Fatalf("GetVATTotals: %v", err)
	}
	// TTC: product line 1000 + delivery fee 500 = 1500.
	if totals.TotalTTCCents != 1500 {
		t.Fatalf("expected TTC=1500 (1000 product + 500 delivery fee), got %d", totals.TotalTTCCents)
	}
	// HT: 1000*100/120=833.33 + 500*100/120=416.67 = 1250.00 exactly (unlike
	// the two summed independently: 833+417=1250 — chosen numbers avoid
	// masking a real rounding bug behind a coincidental exact match, verified
	// by also checking the raw unrounded sum below via by_rate).
	if totals.TotalHTCents != 1250 {
		t.Fatalf("expected HT=1250 (833+417, product line + delivery fee, both @ 20%%), got %d", totals.TotalHTCents)
	}

	byRateShares, err := repo.GetVATByRate(ctx, []string{merchantID}, periodStartUTC, periodEndUTC)
	if err != nil {
		t.Fatalf("GetVATByRate: %v", err)
	}
	byRate := apportionVATByRate(byRateShares, totals.TotalHTCents)
	if len(byRate) != 1 {
		t.Fatalf("expected exactly one rate group (20%%, product line and delivery fee share the same rate value), got %+v", byRate)
	}
	if byRate[0].Rate != 20 {
		t.Fatalf("expected rate=20, got %v", byRate[0].Rate)
	}
	if byRate[0].BaseHTCents != 1250 {
		t.Fatalf("expected by_rate[20].BaseHTCents=1250 (product line HT + delivery fee HT merged into the same rate bucket), got %d", byRate[0].BaseHTCents)
	}

	byChannelShares, err := repo.GetVATByChannel(ctx, []string{merchantID}, periodStartUTC, periodEndUTC)
	if err != nil {
		t.Fatalf("GetVATByChannel: %v", err)
	}
	byChannel := apportionVATByChannel(byChannelShares, totals.TotalHTCents)
	channelHT := map[string]int64{}
	for _, c := range byChannel {
		channelHT[c.Channel] = c.BaseHTCents
	}
	if channelHT[ChannelDelivery] != 1250 {
		t.Fatalf("expected delivery channel HT=1250 (product line + its own order's delivery fee, both attributed to the same order's channel), got %+v", channelHT)
	}

	// A delivery-fee-free order in the same period must not pick up any
	// phantom delivery-fee contribution — the UNION ALL branch is filtered
	// to delivery_fees > 0.
	orderBID := seedOrderWithDeliveryFees(t, ctx, db, merchantID, 302, "WELLO_RESTO", "ACCEPTED", "CLOSED", "IN", 600, 0, orderTime)
	seedOrderItem(t, ctx, db, orderBID, productID, merchantID, 1, 600)

	totalsAfter, err := repo.GetVATTotals(ctx, []string{merchantID}, periodStartUTC, periodEndUTC)
	if err != nil {
		t.Fatalf("GetVATTotals (after order B): %v", err)
	}
	if totalsAfter.TotalTTCCents != 1500+600 {
		t.Fatalf("expected TTC=%d (order A's 1500 + order B's 600 product line, no phantom fee), got %d", 1500+600, totalsAfter.TotalTTCCents)
	}
}

func seedOrderWithDeliveryFees(t *testing.T, ctx context.Context, db *sql.DB, merchantID string, orderNum int, brand, brandStatus, state, orderType string, priceCents, deliveryFeesCents int64, creationTime time.Time) int64 {
	t.Helper()
	var orderID int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, ht, tva, delivery_fees, created_by, creation_date)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0, 0, $8, 'itest-analytics-vdf', $9)
		RETURNING order_id`,
		merchantID, orderNum, brand, brandStatus, state, orderType, priceCents, deliveryFeesCents, creationTime.UTC(),
	).Scan(&orderID)
	if err != nil {
		t.Fatalf("seed order with delivery fees (%s/%s/%s): %v", brand, state, orderType, err)
	}
	return orderID
}
