//go:build postgres_integration

package analytics

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestGetVATTotalsThreePeriods_Postgres is PROMPT 25 Phase 4's non-regression
// test for the TVA tab's fusion: GetVATTotalsThreePeriods must match three
// separate GetVATTotals calls exactly, across both UNION ALL branches
// (product lines and delivery fees — see GetVATTotals' doc comment), and must
// respect the [start, end) boundary.
func TestGetVATTotalsThreePeriods_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID, tvaID20 int64
	var product int64
	const merchantTZ = "Europe/Paris"
	cleanup := func() {
		if merchantIntID != 0 {
			mid := itoa(merchantIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
		if product != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE product_id = $1`, product)
		}
		if tvaID20 != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM tva_categories WHERE tva_id = $1`, tvaID20)
		}
	}
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest VATMultiPeriod Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-vmp', 'https://example.com', '0600000000', 'tok-vmp', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('0', 'ITest VATMultiPeriod TVA 20', 'itest', 20) RETURNING tva_id`).Scan(&tvaID20); err != nil {
		t.Fatalf("seed tva_categories: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, category, price, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'ITest VATMultiPeriod Product', 'itest-cat', 1000, $2, $2, $2) RETURNING product_id`,
		merchantID, tvaID20).Scan(&product); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	current := PeriodWindow{Start: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC(), End: time.Date(2026, 6, 8, 0, 0, 0, 0, loc).UTC()}
	previous := PeriodWindow{Start: time.Date(2026, 5, 25, 0, 0, 0, 0, loc).UTC(), End: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC()}
	previousYear := PeriodWindow{Start: time.Date(2025, 6, 1, 0, 0, 0, 0, loc).UTC(), End: time.Date(2025, 6, 8, 0, 0, 0, 0, loc).UTC()}

	// Current: one order with a product line (TTC 1000, HT 833) AND a
	// delivery fee (200 cents, HT 167) — exercises both UNION ALL branches.
	orderCurrentID := seedOrderWithDeliveryFees(t, ctx, db, merchantID, 501, "WELLO_RESTO", "ACCEPTED", "DONE", "DELIVERY", 1000, 200,
		time.Date(2026, 6, 3, 12, 0, 0, 0, loc))
	seedOrderItem(t, ctx, db, orderCurrentID, product, merchantID, 1, 1000)

	orderPreviousID := seedOrder(t, ctx, db, merchantID, 502, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1200,
		time.Date(2026, 5, 27, 12, 0, 0, 0, loc))
	seedOrderItem(t, ctx, db, orderPreviousID, product, merchantID, 1, 1200)

	orderPreviousYearID := seedOrder(t, ctx, db, merchantID, 503, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 600,
		time.Date(2025, 6, 3, 12, 0, 0, 0, loc))
	seedOrderItem(t, ctx, db, orderPreviousYearID, product, merchantID, 1, 600)

	repo := NewRepository(db)

	oldC, err := repo.GetVATTotals(ctx, []string{merchantID}, current.Start, current.End)
	if err != nil {
		t.Fatalf("GetVATTotals (current): %v", err)
	}
	oldP, err := repo.GetVATTotals(ctx, []string{merchantID}, previous.Start, previous.End)
	if err != nil {
		t.Fatalf("GetVATTotals (previous): %v", err)
	}
	oldY, err := repo.GetVATTotals(ctx, []string{merchantID}, previousYear.Start, previousYear.End)
	if err != nil {
		t.Fatalf("GetVATTotals (previousYear): %v", err)
	}

	newC, newP, newY, err := repo.GetVATTotalsThreePeriods(ctx, []string{merchantID}, current, previous, previousYear)
	if err != nil {
		t.Fatalf("GetVATTotalsThreePeriods: %v", err)
	}

	if newC != oldC {
		t.Fatalf("current mismatch — merged=%+v unmerged=%+v", newC, oldC)
	}
	if newP != oldP {
		t.Fatalf("previous mismatch — merged=%+v unmerged=%+v", newP, oldP)
	}
	if newY != oldY {
		t.Fatalf("previousYear mismatch — merged=%+v unmerged=%+v", newY, oldY)
	}

	// 1000*100/120=833.33->833 (product line) + 200*100/120=166.66->167 (delivery fee) = 1000.
	wantCurrentTTC := int64(1000 + 200)
	wantCurrentHT := int64(833 + 167)
	if newC.TotalTTCCents != wantCurrentTTC || newC.TotalHTCents != wantCurrentHT {
		t.Fatalf("expected current TTC=%d/HT=%d, got %+v", wantCurrentTTC, wantCurrentHT, newC)
	}
}
