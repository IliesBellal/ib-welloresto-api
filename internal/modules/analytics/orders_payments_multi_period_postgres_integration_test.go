//go:build postgres_integration

package analytics

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestGetOrdersTotalsThreePeriods_Postgres is PROMPT 25 Phase 4's
// non-regression test for the Commandes tab's fusion: GetOrdersTotalsThreePeriods
// must return exactly what three separate GetOrdersTotals calls returned,
// including the covers sub-aggregates (FILTER within FILTER: places_settings>0
// AND the window), and must respect the [start, end) boundary after the
// OR-of-windows merge.
func TestGetOrdersTotalsThreePeriods_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	const merchantTZ = "Europe/Paris"
	cleanup := func() {
		if merchantIntID != 0 {
			mid := itoa(merchantIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
	}
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest OrdersMultiPeriod Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-omp', 'https://example.com', '0600000000', 'tok-omp', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	current := PeriodWindow{Start: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC(), End: time.Date(2026, 6, 8, 0, 0, 0, 0, loc).UTC()}
	previous := PeriodWindow{Start: time.Date(2026, 5, 25, 0, 0, 0, 0, loc).UTC(), End: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC()}
	previousYear := PeriodWindow{Start: time.Date(2025, 6, 1, 0, 0, 0, 0, loc).UTC(), End: time.Date(2025, 6, 8, 0, 0, 0, 0, loc).UTC()}

	// Current: one order with covers (2), one without.
	seedOrderWithCovers(t, ctx, db, merchantID, 301, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1000, 2, time.Date(2026, 6, 3, 12, 0, 0, 0, loc))
	seedOrder(t, ctx, db, merchantID, 302, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 500, time.Date(2026, 6, 4, 12, 0, 0, 0, loc))

	// Previous: one order, no covers.
	seedOrder(t, ctx, db, merchantID, 303, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 700, time.Date(2026, 5, 27, 12, 0, 0, 0, loc))

	// Previous year: one order.
	seedOrder(t, ctx, db, merchantID, 304, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 900, time.Date(2025, 6, 3, 12, 0, 0, 0, loc))

	// Boundary: at current.End (excluded), at current.Start (included).
	seedOrder(t, ctx, db, merchantID, 305, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 9999, current.End.In(loc))
	seedOrder(t, ctx, db, merchantID, 306, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 250, current.Start.In(loc))

	repo := NewRepository(db)

	oldC, err := repo.GetOrdersTotals(ctx, []string{merchantID}, current.Start, current.End)
	if err != nil {
		t.Fatalf("GetOrdersTotals (current): %v", err)
	}
	oldP, err := repo.GetOrdersTotals(ctx, []string{merchantID}, previous.Start, previous.End)
	if err != nil {
		t.Fatalf("GetOrdersTotals (previous): %v", err)
	}
	oldY, err := repo.GetOrdersTotals(ctx, []string{merchantID}, previousYear.Start, previousYear.End)
	if err != nil {
		t.Fatalf("GetOrdersTotals (previousYear): %v", err)
	}

	newC, newP, newY, err := repo.GetOrdersTotalsThreePeriods(ctx, []string{merchantID}, current, previous, previousYear)
	if err != nil {
		t.Fatalf("GetOrdersTotalsThreePeriods: %v", err)
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

	// Explicit expected values: 250 (Start, included) + 1000 (covers) + 500 = 1750, 3 orders; End excluded.
	if newC.OrderCount != 3 || newC.TotalTTCCents != 1750 {
		t.Fatalf("expected current OrderCount=3/TTC=1750 (boundary at End excluded), got %+v", newC)
	}
	if newC.OrdersWithCovers != 1 || newC.TotalCovers != 2 || newC.TTCCentsOfOrdersWithCovers != 1000 {
		t.Fatalf("expected current covers OrdersWithCovers=1/TotalCovers=2/TTC=1000, got %+v", newC)
	}
	if newP.OrderCount != 1 || newP.TotalTTCCents != 700 {
		t.Fatalf("expected previous OrderCount=1/TTC=700, got %+v", newP)
	}
	if newY.OrderCount != 1 || newY.TotalTTCCents != 900 {
		t.Fatalf("expected previousYear OrderCount=1/TTC=900, got %+v", newY)
	}
}

// TestGetPaymentsTotalsThreePeriods_Postgres is PROMPT 25 Phase 4's
// non-regression test for the Règlements tab's fusion — same shape as
// TestGetOrdersTotalsThreePeriods_Postgres, plus a disabled payment
// (payments.enabled = FALSE) that must be excluded from every window.
func TestGetPaymentsTotalsThreePeriods_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	const merchantTZ = "Europe/Paris"
	cleanup := func() {
		if merchantIntID != 0 {
			mid := itoa(merchantIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM payments WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
	}
	t.Cleanup(cleanup)

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest PaymentsMultiPeriod Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-pmp', 'https://example.com', '0600000000', 'tok-pmp', $1)
		RETURNING id`, merchantTZ).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := itoa(merchantIntID)

	loc, err := time.LoadLocation(merchantTZ)
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	current := PeriodWindow{Start: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC(), End: time.Date(2026, 6, 8, 0, 0, 0, 0, loc).UTC()}
	previous := PeriodWindow{Start: time.Date(2026, 5, 25, 0, 0, 0, 0, loc).UTC(), End: time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC()}
	previousYear := PeriodWindow{Start: time.Date(2025, 6, 1, 0, 0, 0, 0, loc).UTC(), End: time.Date(2025, 6, 8, 0, 0, 0, 0, loc).UTC()}

	orderCurrent := seedOrder(t, ctx, db, merchantID, 401, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1000, time.Date(2026, 6, 3, 12, 0, 0, 0, loc))
	seedPayment(t, ctx, db, merchantID, orderCurrent, "CB", 1000, true)

	orderCurrentDisabled := seedOrder(t, ctx, db, merchantID, 402, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 300, time.Date(2026, 6, 3, 13, 0, 0, 0, loc))
	seedPayment(t, ctx, db, merchantID, orderCurrentDisabled, "CB", 300, false) // disabled — must be excluded

	orderPrevious := seedOrder(t, ctx, db, merchantID, 403, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 700, time.Date(2026, 5, 27, 12, 0, 0, 0, loc))
	seedPayment(t, ctx, db, merchantID, orderPrevious, "ES", 700, true)

	orderPreviousYear := seedOrder(t, ctx, db, merchantID, 404, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 900, time.Date(2025, 6, 3, 12, 0, 0, 0, loc))
	seedPayment(t, ctx, db, merchantID, orderPreviousYear, "CB", 900, true)

	repo := NewRepository(db)

	oldC, err := repo.GetPaymentsTotals(ctx, []string{merchantID}, current.Start, current.End)
	if err != nil {
		t.Fatalf("GetPaymentsTotals (current): %v", err)
	}
	oldP, err := repo.GetPaymentsTotals(ctx, []string{merchantID}, previous.Start, previous.End)
	if err != nil {
		t.Fatalf("GetPaymentsTotals (previous): %v", err)
	}
	oldY, err := repo.GetPaymentsTotals(ctx, []string{merchantID}, previousYear.Start, previousYear.End)
	if err != nil {
		t.Fatalf("GetPaymentsTotals (previousYear): %v", err)
	}

	newC, newP, newY, err := repo.GetPaymentsTotalsThreePeriods(ctx, []string{merchantID}, current, previous, previousYear)
	if err != nil {
		t.Fatalf("GetPaymentsTotalsThreePeriods: %v", err)
	}

	if newC != oldC || newP != oldP || newY != oldY {
		t.Fatalf("mismatch — merged current=%+v previous=%+v previousYear=%+v vs unmerged current=%+v previous=%+v previousYear=%+v",
			newC, newP, newY, oldC, oldP, oldY)
	}
	if newC.TotalAmountCents != 1000 || newC.PaymentCount != 1 {
		t.Fatalf("expected current TotalAmountCents=1000/PaymentCount=1 (disabled payment excluded), got %+v", newC)
	}
	if newP.TotalAmountCents != 700 || newP.PaymentCount != 1 {
		t.Fatalf("expected previous TotalAmountCents=700/PaymentCount=1, got %+v", newP)
	}
	if newY.TotalAmountCents != 900 || newY.PaymentCount != 1 {
		t.Fatalf("expected previousYear TotalAmountCents=900/PaymentCount=1, got %+v", newY)
	}
}
