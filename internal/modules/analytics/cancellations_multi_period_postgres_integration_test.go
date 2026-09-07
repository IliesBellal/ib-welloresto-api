//go:build postgres_integration

package analytics

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestGetOrdersCreatedCountThreePeriods_Postgres and
// TestGetCancellationsTotalsThreePeriods_Postgres are PROMPT 25 Phase 4's
// non-regression tests for the Annulations tab's fusion — the two queries
// cancellationsPeriodTotalsThreePeriods now runs once each (instead of once
// per period, three times) must still match the unmerged calls exactly, and
// respect the [start, end) boundary and the CANCELED-only scope
// (AnalyticsCancellationsScopeMultiPeriod vs
// AnalyticsAllOrdersCreatedScopeMultiPeriod are deliberately different scopes
// — see scope.go).
func TestGetOrdersCreatedCountThreePeriods_Postgres(t *testing.T) {
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
		VALUES ('ITest CancelMultiPeriod Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-cmp', 'https://example.com', '0600000000', 'tok-cmp', $1)
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

	// GetOrdersCreatedCount counts EVERY order created, any state/brand_status
	// (AnalyticsAllOrdersCreatedScope) — a cancelled order still counts here.
	seedOrder(t, ctx, db, merchantID, 601, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1000, time.Date(2026, 6, 3, 12, 0, 0, 0, loc))
	seedCancelOrder(t, ctx, db, merchantID, 602, "WELLO_RESTO", "CANCELED", "IN", 500, time.Date(2026, 6, 4, 12, 0, 0, 0, loc), "itest-analytics", strPtr("STAFF"), nil)
	seedOrder(t, ctx, db, merchantID, 603, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 700, time.Date(2026, 5, 27, 12, 0, 0, 0, loc))
	seedOrder(t, ctx, db, merchantID, 604, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 900, time.Date(2025, 6, 3, 12, 0, 0, 0, loc))
	// Boundary.
	seedOrder(t, ctx, db, merchantID, 605, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 9999, current.End.In(loc))
	seedOrder(t, ctx, db, merchantID, 606, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 250, current.Start.In(loc))

	repo := NewRepository(db)

	oldC, err := repo.GetOrdersCreatedCount(ctx, []string{merchantID}, current.Start, current.End)
	if err != nil {
		t.Fatalf("GetOrdersCreatedCount (current): %v", err)
	}
	oldP, err := repo.GetOrdersCreatedCount(ctx, []string{merchantID}, previous.Start, previous.End)
	if err != nil {
		t.Fatalf("GetOrdersCreatedCount (previous): %v", err)
	}
	oldY, err := repo.GetOrdersCreatedCount(ctx, []string{merchantID}, previousYear.Start, previousYear.End)
	if err != nil {
		t.Fatalf("GetOrdersCreatedCount (previousYear): %v", err)
	}

	newC, newP, newY, err := repo.GetOrdersCreatedCountThreePeriods(ctx, []string{merchantID}, current, previous, previousYear)
	if err != nil {
		t.Fatalf("GetOrdersCreatedCountThreePeriods: %v", err)
	}

	if newC != oldC || newP != oldP || newY != oldY {
		t.Fatalf("mismatch — merged current=%v previous=%v previousYear=%v vs unmerged current=%v previous=%v previousYear=%v",
			newC, newP, newY, oldC, oldP, oldY)
	}
	// Start included, End excluded: orders 601 (DONE), 602 (CANCELED, still
	// counted here), 606 (at Start) = 3. Order 605 (at End) excluded.
	if newC != 3 {
		t.Fatalf("expected current count=3 (boundary at End excluded, CANCELED order still counted), got %d", newC)
	}
	if newP != 1 || newY != 1 {
		t.Fatalf("expected previous=1/previousYear=1, got previous=%d previousYear=%d", newP, newY)
	}
}

func TestGetCancellationsTotalsThreePeriods_Postgres(t *testing.T) {
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
		VALUES ('ITest CancelTotalsMultiPeriod Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-ctmp', 'https://example.com', '0600000000', 'tok-ctmp', $1)
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

	// A DONE order (not cancelled — must never appear in cancellations totals).
	seedOrder(t, ctx, db, merchantID, 701, "WELLO_RESTO", "ACCEPTED", "DONE", "IN", 1000, time.Date(2026, 6, 3, 12, 0, 0, 0, loc))
	// Current: one STAFF cancellation, one PLATFORM cancellation.
	seedCancelOrder(t, ctx, db, merchantID, 702, "WELLO_RESTO", "CANCELED", "IN", 500, time.Date(2026, 6, 4, 12, 0, 0, 0, loc), "itest-analytics", strPtr("STAFF"), nil)
	seedCancelOrder(t, ctx, db, merchantID, 703, "WELLO_RESTO", "CANCELED", "IN", 300, time.Date(2026, 6, 5, 12, 0, 0, 0, loc), "itest-analytics", strPtr("PLATFORM"), nil)
	// Previous: one CUSTOMER cancellation.
	seedCancelOrder(t, ctx, db, merchantID, 704, "WELLO_RESTO", "CANCELED", "IN", 700, time.Date(2026, 5, 27, 12, 0, 0, 0, loc), "itest-analytics", strPtr("CUSTOMER"), nil)
	// Previous year: one unknown-author cancellation.
	seedCancelOrder(t, ctx, db, merchantID, 705, "WELLO_RESTO", "CANCELED", "IN", 900, time.Date(2025, 6, 3, 12, 0, 0, 0, loc), "itest-analytics", nil, nil)

	repo := NewRepository(db)

	oldC, err := repo.GetCancellationsTotals(ctx, []string{merchantID}, current.Start, current.End)
	if err != nil {
		t.Fatalf("GetCancellationsTotals (current): %v", err)
	}
	oldP, err := repo.GetCancellationsTotals(ctx, []string{merchantID}, previous.Start, previous.End)
	if err != nil {
		t.Fatalf("GetCancellationsTotals (previous): %v", err)
	}
	oldY, err := repo.GetCancellationsTotals(ctx, []string{merchantID}, previousYear.Start, previousYear.End)
	if err != nil {
		t.Fatalf("GetCancellationsTotals (previousYear): %v", err)
	}

	newC, newP, newY, err := repo.GetCancellationsTotalsThreePeriods(ctx, []string{merchantID}, current, previous, previousYear)
	if err != nil {
		t.Fatalf("GetCancellationsTotalsThreePeriods: %v", err)
	}

	if newC != oldC || newP != oldP || newY != oldY {
		t.Fatalf("mismatch — merged current=%+v previous=%+v previousYear=%+v vs unmerged current=%+v previous=%+v previousYear=%+v",
			newC, newP, newY, oldC, oldP, oldY)
	}
	if newC.CancelledCount != 2 || newC.CancelledAmountCents != 800 || newC.InternalCancelledCount != 1 || newC.PlatformCancelledCount != 1 || newC.StaffCancelledCount != 1 {
		t.Fatalf("expected current CancelledCount=2/Amount=800/Internal=1/Platform=1/Staff=1, got %+v", newC)
	}
	if newP.CancelledCount != 1 || newP.InternalCancelledCount != 1 {
		t.Fatalf("expected previous CancelledCount=1/Internal=1 (CUSTOMER is internal), got %+v", newP)
	}
	if newY.CancelledCount != 1 || newY.UnknownCancelledCount != 1 {
		t.Fatalf("expected previousYear CancelledCount=1/Unknown=1, got %+v", newY)
	}
}
