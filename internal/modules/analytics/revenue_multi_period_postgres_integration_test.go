//go:build postgres_integration

package analytics

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestGetRevenueTotalsThreePeriods_Postgres is PROMPT 25 Phase 4's mandatory
// non-regression test for the CA tab's fusion: GetRevenueTotalsThreePeriods
// (one query, FILTER per window) must return EXACTLY what three separate
// GetRevenueTotalsTTC calls (and, when includeHT, three GetRevenueTotalsHT
// calls) already returned — never an approximation, never a number that
// merely looks right. Covers:
//   - three independent windows (current/previous/previous-year), each with
//     its own order, agreeing with the old per-window calls;
//   - the [start, end) boundary surviving the OR-of-windows merge exactly: an
//     order at a window's End is excluded, one at its Start is included, for
//     both the current and the previous window — this is the specific class
//     of bug ("a window boundary shifted by one merge, silently") this test
//     exists to catch, not just a general sanity check;
//   - a window that itself spans the 2026-03-29/30 French DST transition,
//     the same date this package's timeline bug already hit once
//     (TestRepository_Postgres's dstOrderID case) — the fusion here doesn't
//     touch local-day bucketing, but the boundary arithmetic must still
//     agree with the unmerged path on a DST-adjacent window;
//   - includeHT=false takes the cheap orders-only path and never returns a
//     nonzero HT.
func TestGetRevenueTotalsThreePeriods_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	var tvaID20 int64
	var product int64
	const merchantTZ = "Europe/Paris"

	cleanup := func() {
		if merchantIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE merchant_id = $1`, itoa(merchantIntID))
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, itoa(merchantIntID))
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
		VALUES ('ITest MultiPeriod Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-multiperiod', 'https://example.com', '0600000000', 'tok-multiperiod', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO tva_categories (delivery_type, tva_title, tva_desc, tva_rate)
		VALUES ('0', 'ITest MultiPeriod TVA 20', 'itest', 20) RETURNING tva_id`).Scan(&tvaID20); err != nil {
		t.Fatalf("seed tva_categories: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_id, name, category, price, tva_in_id, tva_take_away_id, tva_delivery_id)
		VALUES ($1, 'ITest MultiPeriod Product', 'itest-cat', 1000, $2, $2, $2) RETURNING product_id`,
		merchantID, tvaID20).Scan(&product); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	current := PeriodWindow{
		Start: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC(),
		End:   time.Date(2026, 6, 8, 0, 0, 0, 0, loc).UTC(),
	}
	previous := PeriodWindow{
		Start: time.Date(2026, 5, 25, 0, 0, 0, 0, loc).UTC(),
		End:   time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC(),
	}
	previousYear := PeriodWindow{
		Start: time.Date(2025, 6, 1, 0, 0, 0, 0, loc).UTC(),
		End:   time.Date(2025, 6, 8, 0, 0, 0, 0, loc).UTC(),
	}

	// One order per window, TTC 1000, tva_rate 20 -> HT = 1000*100/120 = 833.33... rounds to 833.
	orderCurrentID := seedOrder(t, ctx, db, merchantID, 201, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1000,
		time.Date(2026, 6, 3, 12, 0, 0, 0, loc))
	seedOrderItem(t, ctx, db, orderCurrentID, product, merchantID, 1, 1000)

	orderPreviousID := seedOrder(t, ctx, db, merchantID, 202, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1000,
		time.Date(2026, 5, 27, 12, 0, 0, 0, loc))
	seedOrderItem(t, ctx, db, orderPreviousID, product, merchantID, 1, 1000)

	orderPreviousYearID := seedOrder(t, ctx, db, merchantID, 203, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1000,
		time.Date(2025, 6, 3, 12, 0, 0, 0, loc))
	seedOrderItem(t, ctx, db, orderPreviousYearID, product, merchantID, 1, 1000)

	// Boundary orders: one exactly at current.End (must be excluded — [start,end)
	// is half-open), one exactly at current.Start (must be included).
	orderAtEndID := seedOrder(t, ctx, db, merchantID, 204, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 9999,
		current.End.In(loc))
	seedOrderItem(t, ctx, db, orderAtEndID, product, merchantID, 1, 9999)

	orderAtStartID := seedOrder(t, ctx, db, merchantID, 205, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 500,
		current.Start.In(loc))
	seedOrderItem(t, ctx, db, orderAtStartID, product, merchantID, 1, 500)

	repo := NewRepository(db)

	// --- Core agreement check: the merged query vs the three unmerged calls,
	// for both includeHT branches. ---
	for _, includeHT := range []bool{true, false} {
		oldCurrent, err := repo.GetRevenueTotalsTTC(ctx, []string{merchantID}, current.Start, current.End)
		if err != nil {
			t.Fatalf("GetRevenueTotalsTTC (current): %v", err)
		}
		oldPrevious, err := repo.GetRevenueTotalsTTC(ctx, []string{merchantID}, previous.Start, previous.End)
		if err != nil {
			t.Fatalf("GetRevenueTotalsTTC (previous): %v", err)
		}
		oldPreviousYear, err := repo.GetRevenueTotalsTTC(ctx, []string{merchantID}, previousYear.Start, previousYear.End)
		if err != nil {
			t.Fatalf("GetRevenueTotalsTTC (previousYear): %v", err)
		}
		if includeHT {
			htCurrent, err := repo.GetRevenueTotalsHT(ctx, []string{merchantID}, current.Start, current.End)
			if err != nil {
				t.Fatalf("GetRevenueTotalsHT (current): %v", err)
			}
			htPrevious, err := repo.GetRevenueTotalsHT(ctx, []string{merchantID}, previous.Start, previous.End)
			if err != nil {
				t.Fatalf("GetRevenueTotalsHT (previous): %v", err)
			}
			htPreviousYear, err := repo.GetRevenueTotalsHT(ctx, []string{merchantID}, previousYear.Start, previousYear.End)
			if err != nil {
				t.Fatalf("GetRevenueTotalsHT (previousYear): %v", err)
			}
			oldCurrent.TotalHTCents = htCurrent
			oldPrevious.TotalHTCents = htPrevious
			oldPreviousYear.TotalHTCents = htPreviousYear
		}

		newCurrent, newPrevious, newPreviousYear, err := repo.GetRevenueTotalsThreePeriods(ctx, []string{merchantID}, current, previous, previousYear, includeHT)
		if err != nil {
			t.Fatalf("GetRevenueTotalsThreePeriods (includeHT=%v): %v", includeHT, err)
		}

		if newCurrent != oldCurrent {
			t.Fatalf("includeHT=%v: current period mismatch — merged=%+v unmerged=%+v", includeHT, newCurrent, oldCurrent)
		}
		if newPrevious != oldPrevious {
			t.Fatalf("includeHT=%v: previous period mismatch — merged=%+v unmerged=%+v", includeHT, newPrevious, oldPrevious)
		}
		if newPreviousYear != oldPreviousYear {
			t.Fatalf("includeHT=%v: previous-year period mismatch — merged=%+v unmerged=%+v", includeHT, newPreviousYear, oldPreviousYear)
		}

		// --- Explicit expected values, not just "matches the old path" —
		// catches a bug that happens to be present identically on both sides. ---
		if newCurrent.TotalTTCCents != 1500 || newCurrent.OrderCount != 2 {
			t.Fatalf("includeHT=%v: expected current TTC=1500/count=2 (order at Start included, order at End excluded), got %+v", includeHT, newCurrent)
		}
		if newPrevious.TotalTTCCents != 1000 || newPrevious.OrderCount != 1 {
			t.Fatalf("includeHT=%v: expected previous TTC=1000/count=1, got %+v", includeHT, newPrevious)
		}
		if newPreviousYear.TotalTTCCents != 1000 || newPreviousYear.OrderCount != 1 {
			t.Fatalf("includeHT=%v: expected previousYear TTC=1000/count=1, got %+v", includeHT, newPreviousYear)
		}
		if !includeHT {
			if newCurrent.TotalHTCents != 0 || newPrevious.TotalHTCents != 0 || newPreviousYear.TotalHTCents != 0 {
				t.Fatalf("includeHT=false must never compute HT, got current=%+v previous=%+v previousYear=%+v", newCurrent, newPrevious, newPreviousYear)
			}
		} else {
			// 1000 * 100/120 = 833.33... rounds to 833, once per order.
			if newCurrent.TotalHTCents != 833*2 {
				t.Fatalf("expected current HT=%d (2 orders at 833 each), got %d", 833*2, newCurrent.TotalHTCents)
			}
			if newPrevious.TotalHTCents != 833 || newPreviousYear.TotalHTCents != 833 {
				t.Fatalf("expected previous/previousYear HT=833 each, got previous=%d previousYear=%d", newPrevious.TotalHTCents, newPreviousYear.TotalHTCents)
			}
		}
	}

	// --- DST-spanning window: 2026-03-29/30 is the exact date this package's
	// timeline bug already hit once (TestRepository_Postgres). This fusion
	// doesn't bucket by local day, but the window's own boundary arithmetic
	// (Go-side, in service.go, unchanged by this fusion) must still agree
	// with the unmerged path when it happens to straddle a DST transition. ---
	dstCurrent := PeriodWindow{
		Start: time.Date(2026, 3, 28, 0, 0, 0, 0, loc).UTC(),
		End:   time.Date(2026, 3, 31, 0, 0, 0, 0, loc).UTC(),
	}
	dstOrderID := seedOrder(t, ctx, db, merchantID, 206, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 700,
		time.Date(2026, 3, 30, 0, 30, 0, 0, loc))
	seedOrderItem(t, ctx, db, dstOrderID, product, merchantID, 1, 700)

	oldDST, err := repo.GetRevenueTotalsTTC(ctx, []string{merchantID}, dstCurrent.Start, dstCurrent.End)
	if err != nil {
		t.Fatalf("GetRevenueTotalsTTC (DST window): %v", err)
	}
	newDST, _, _, err := repo.GetRevenueTotalsThreePeriods(ctx, []string{merchantID}, dstCurrent, previous, previousYear, false)
	if err != nil {
		t.Fatalf("GetRevenueTotalsThreePeriods (DST window): %v", err)
	}
	if newDST != oldDST {
		t.Fatalf("DST-spanning window mismatch — merged=%+v unmerged=%+v", newDST, oldDST)
	}
	if newDST.TotalTTCCents != 700 || newDST.OrderCount != 1 {
		t.Fatalf("expected DST window TTC=700/count=1, got %+v", newDST)
	}
}
