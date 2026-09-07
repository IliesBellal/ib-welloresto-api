//go:build postgres_integration

package customers

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestStatsReconciliation_Postgres exercises PROMPT 26 Phase 4's detector:
// SampleCustomerStatsDrift must flag a customer whose cached counters were
// deliberately corrupted (simulating redrift) and leave an untouched
// customer unflagged, and RecordStatsReconciliationRun must leave the
// durable trace this task's own doc comment promises (the cron
// infrastructure otherwise keeps none — CLAUDE.md).
func TestStatsReconciliation_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const merchantID = "999929"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM customer WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewCustomerRepository(db)

	var goodID, driftedID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO customer (merchant_id, customer_name, customer_brand, customer_nb_orders, customer_total_spent)
		VALUES ($1, 'itest good', 'WELLO_RESTO', 1, 1000) RETURNING customer_id`, merchantID).Scan(&goodID); err != nil {
		t.Fatalf("seed good customer: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO customer (merchant_id, customer_name, customer_brand, customer_nb_orders, customer_total_spent)
		VALUES ($1, 'itest drifted', 'WELLO_RESTO', 5, 9999) RETURNING customer_id`, merchantID).Scan(&driftedID); err != nil {
		t.Fatalf("seed drifted customer: %v", err)
	}

	// The "good" customer's cached stats actually match its one qualifying
	// order — including last_order_date, set to the order's own
	// creation_date after insert so the two match exactly (not just
	// approximately via two separate now() calls).
	var goodOrderID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, customer_id, order_num, brand, brand_status, order_type, state, price, TVA, HT, created_by)
		VALUES ($1, $2, 1, 'WELLO_RESTO', 'ACCEPTED', 'IN', 'CLOSED', 1000, 0, 1000, 'itest')
		RETURNING order_id`, merchantID, goodID).Scan(&goodOrderID); err != nil {
		t.Fatalf("seed good order: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE customer SET last_order_date = (SELECT creation_date FROM orders WHERE order_id = $1)
		WHERE customer_id = $2`, goodOrderID, goodID); err != nil {
		t.Fatalf("align good customer last_order_date: %v", err)
	}
	// The "drifted" customer has no qualifying order at all — its cached
	// stats (5 / 9999) are pure drift, simulating exactly the kind of
	// redrift this task exists to catch after the fact.

	goodStr := strconv.FormatInt(goodID, 10)
	driftedStr := strconv.FormatInt(driftedID, 10)

	sample, err := repo.SampleCustomerStatsDrift(ctx, 10, []string{goodStr, driftedStr})
	if err != nil {
		t.Fatalf("SampleCustomerStatsDrift failed against postgres: %v", err)
	}
	if sample.SampleSize != 2 {
		t.Fatalf("expected sample size 2, got %d", sample.SampleSize)
	}
	if sample.Mismatches != 1 {
		t.Fatalf("expected exactly 1 mismatch (the drifted customer), got %d", sample.Mismatches)
	}
	if sample.MaxNbOrdersDiff != 5 { // 5 stored vs 0 recomputed (no qualifying order)
		t.Fatalf("expected max_nb_orders_diff=5, got %d", sample.MaxNbOrdersDiff)
	}
	if sample.MaxTotalSpentDiffAbs != 9999 {
		t.Fatalf("expected max_total_spent_diff=9999, got %d", sample.MaxTotalSpentDiffAbs)
	}

	// --- durable trace (PROMPT 26 Phase 4 : le cron n'a aucun journal propre) ---
	if err := repo.RecordStatsReconciliationRun(ctx, sample, 0.01, true, 42); err != nil {
		t.Fatalf("RecordStatsReconciliationRun failed against postgres: %v", err)
	}
	var runID int64
	var sampleSize, mismatches int
	var alertTriggered bool
	if err := db.QueryRowContext(ctx, `
		SELECT id, sample_size, mismatches, alert_triggered
		FROM customer_stats_reconciliation_runs
		ORDER BY id DESC LIMIT 1`).Scan(&runID, &sampleSize, &mismatches, &alertTriggered); err != nil {
		t.Fatalf("read back reconciliation run: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM customer_stats_reconciliation_runs WHERE id = $1`, runID) })
	if sampleSize != 2 || mismatches != 1 || !alertTriggered {
		t.Fatalf("expected persisted run (2, 1, true), got (%d, %d, %v)", sampleSize, mismatches, alertTriggered)
	}
}
