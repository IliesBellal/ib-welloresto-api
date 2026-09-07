package tasks

import (
	"context"
	"time"

	"welloresto-api/internal/modules/customers"

	"go.uber.org/zap"
)

// customerStatsReconciliationSampleSize and ...ThresholdRatio: PROMPT 26
// Phase 4's anti-redrift guard. A sample, not a full scan — 26k customers on
// a 0.1 vCPU instance ruled out a daily full recompute (see
// cmd/backfill_customer_stats's own doc comment); a few hundred rows is
// enough to notice a write-path regression well before it silently
// accumulates the way the original bug did. The threshold is deliberately
// tight: right after Phase 3's backfill the true drift is 0, so any nonzero
// ratio above noise is worth a look, not a reason to raise the bar.
const (
	customerStatsReconciliationSampleSize = 500
	customerStatsReconciliationThreshold  = 0.01 // 1% of the sample
)

// ReconcileCustomerStats is PROMPT 26 Phase 4: a daily sample comparing
// customer.customer_nb_orders/customer_total_spent/last_order_date against a
// live recompute from orders (same scope as ApplyOrderToCustomerStats and
// cmd/backfill_customer_stats). It never writes a correction itself — that's
// cmd/backfill_customer_stats's job, run deliberately, not from a cron.
//
// Every run is persisted to customer_stats_reconciliation_runs (migration
// 122): the only durable trace of this task's execution, since the cron
// infrastructure itself keeps none (see CLAUDE.md). A mismatch ratio above
// customerStatsReconciliationThreshold logs at Error level — this repo has
// no ops paging/Slack integration to hook into, so a log-level signal on an
// already-monitored process is the honest ceiling of what this task can do
// on its own; the persisted row is what makes that signal checkable after
// the fact instead of only livestream-visible.
func (tm *TasksManager) ReconcileCustomerStats() {
	ctx := context.Background()
	repo := customers.NewCustomerRepository(tm.DB)

	started := time.Now()
	sample, err := repo.SampleCustomerStatsDrift(ctx, customerStatsReconciliationSampleSize, nil)
	if err != nil {
		tm.logError("customer stats reconciliation failed", zap.Error(err))
		return
	}
	durationMs := time.Since(started).Milliseconds()

	ratio := 0.0
	if sample.SampleSize > 0 {
		ratio = float64(sample.Mismatches) / float64(sample.SampleSize)
	}
	alertTriggered := ratio > customerStatsReconciliationThreshold

	if err := repo.RecordStatsReconciliationRun(ctx, sample, customerStatsReconciliationThreshold, alertTriggered, durationMs); err != nil {
		// Recording failed, but the check itself ran — log both, since a
		// silent recording failure would make this task invisible again,
		// exactly the gap this task exists to close.
		tm.logError("customer stats reconciliation: failed to persist run", zap.Error(err))
	}

	fields := []zap.Field{
		zap.Int("sample_size", sample.SampleSize),
		zap.Int("mismatches", sample.Mismatches),
		zap.Float64("mismatch_ratio", ratio),
		zap.Int64("max_nb_orders_diff", sample.MaxNbOrdersDiff),
		zap.Int64("max_total_spent_diff_cents", sample.MaxTotalSpentDiffAbs),
		zap.Int64("duration_ms", durationMs),
	}
	if alertTriggered {
		tm.logError("customer stats reconciliation: drift above threshold — customer_nb_orders/customer_total_spent/last_order_date may need cmd/backfill_customer_stats re-run", fields...)
		return
	}
	tm.logInfo("customer stats reconciliation finished", fields...)
}
