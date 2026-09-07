// Command backfill_customer_stats recomputes customer.customer_nb_orders,
// customer.customer_total_spent and customer.last_order_date from `orders`,
// under the definition settled in PROMPT 26 Phase 1 (docs/decisions.md): a
// qualifying order is state IN ('CLOSED','DONE') AND upper(brand_status) NOT
// IN ('DELETED','CANCELED'), across every brand (WELLO_RESTO, UBER_EATS,
// DELIVEROO) — the same scope as internal/modules/analytics's
// AnalyticsOrdersScope, without its brand restriction.
//
// Beyond the 3 cached columns, this tool also stamps
// orders.customer_stats_counted_at on every currently-qualifying order (set
// if unset, cleared if the order no longer qualifies) — the idempotency
// marker PROMPT 26 Phase 2 introduced (migration
// 121_customer_stats_counted_marker) so that a future cancellation
// (DeleteOrderLocal) or reopen (ReopenClosedOrder) correctly reverses
// exactly the orders this backfill just credited. Skipping this step would
// leave every historical order un-marked, silently defeating Phase 2's fix
// for any order closed before this tool ran.
//
// Idempotent: re-running produces the same result and the same "0 changed"
// report the second time. Batched and resumable: processes customers in
// ascending customer_id order, one small transaction per customer (never a
// single long-held lock on `customer`, which the tablets read live), and
// prints the last completed customer_id periodically so an interrupted run
// can resume with --start-after instead of rescanning from zero.
//
// Deliberately not using internal/config.Load()/internal/database: this is
// a standalone, DB-only maintenance tool (same posture as
// cmd/diagnose_migrations) and should not require GOOGLE_API_KEY,
// R2_PRIVATE_BUCKET, PIN_PEPPER, which have nothing to do with it.
//
// Usage:
//
//	# Always run this first — writes nothing, reports the real drift.
//	POSTGRES_URL="postgres://..." go run ./cmd/backfill_customer_stats
//
//	# Apply for real, from the beginning:
//	POSTGRES_URL="postgres://..." go run ./cmd/backfill_customer_stats --apply
//
//	# Resume an interrupted apply run after customer_id 118234:
//	POSTGRES_URL="postgres://..." go run ./cmd/backfill_customer_stats --apply --start-after=118234
//
// See docs/decisions.md (PROMPT 26 Phase 3) for the staging simulation
// result and the production run window/duration estimate.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const batchSize = 500

type customerRow struct {
	customerID string

	storedNbOrders   int64
	storedTotalSpent int64
	storedLastOrder  sql.NullTime

	recomputedNbOrders   int64
	recomputedTotalSpent int64
	recomputedLastOrder  sql.NullTime
}

func (c customerRow) changed() bool {
	return c.storedNbOrders != c.recomputedNbOrders ||
		c.storedTotalSpent != c.recomputedTotalSpent ||
		!sameTime(c.storedLastOrder, c.recomputedLastOrder)
}

func sameTime(a, b sql.NullTime) bool {
	if a.Valid != b.Valid {
		return false
	}
	if !a.Valid {
		return true
	}
	return a.Time.Equal(b.Time)
}

func main() {
	apply := flag.Bool("apply", false, "actually write changes (default: dry-run, writes nothing)")
	startAfter := flag.String("start-after", "", "resume after this customer_id (exclusive) instead of starting from the beginning")
	flag.Parse()

	url := os.Getenv("POSTGRES_URL")
	if url == "" {
		log.Fatal("POSTGRES_URL is not set — point it at staging or production and re-run")
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("ping: %v", err)
	}

	var dbName string
	_ = db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&dbName)

	mode := "DRY-RUN (nothing will be written)"
	if *apply {
		mode = "APPLY (writing changes)"
	}
	fmt.Printf("backfill_customer_stats — database %q — %s\n", dbName, mode)
	if *startAfter != "" {
		fmt.Printf("resuming after customer_id=%s\n", *startAfter)
	}

	started := time.Now()
	lastID := *startAfter

	var (
		totalScanned   int64
		totalChanged   int64
		nbOrdersDelta  int64 // sum of |recomputed - stored|, informational
		totalSpentDrift int64
	)

	for {
		batch, err := fetchBatch(ctx, db, lastID)
		if err != nil {
			log.Fatalf("fetch batch after %q: %v", lastID, err)
		}
		if len(batch) == 0 {
			break
		}

		for _, c := range batch {
			totalScanned++
			if c.changed() {
				totalChanged++
				diff := c.recomputedNbOrders - c.storedNbOrders
				if diff < 0 {
					diff = -diff
				}
				nbOrdersDelta += diff
				spentDiff := c.recomputedTotalSpent - c.storedTotalSpent
				if spentDiff < 0 {
					spentDiff = -spentDiff
				}
				totalSpentDrift += spentDiff

				if *apply {
					if err := applyCustomer(ctx, db, c); err != nil {
						log.Fatalf("apply customer_id=%s: %v", c.customerID, err)
					}
				}
			}
			lastID = c.customerID
		}

		fmt.Printf("... processed %d customer(s), last customer_id=%s (%d changed so far)\n", totalScanned, lastID, totalChanged)
	}

	elapsed := time.Since(started)
	fmt.Println("---")
	fmt.Printf("scanned=%d changed=%d (%.1f%%) unchanged=%d\n",
		totalScanned, totalChanged, pct(totalChanged, totalScanned), totalScanned-totalChanged)
	fmt.Printf("sum |Δ nb_orders|=%d, sum |Δ total_spent|=%d (centimes)\n", nbOrdersDelta, totalSpentDrift)
	fmt.Printf("elapsed=%s\n", elapsed.Round(time.Second))
	if !*apply {
		fmt.Println("This was a DRY-RUN — nothing was written. Re-run with --apply to write these corrections.")
	} else {
		fmt.Println("Applied. customer_stats_counted_at is now stamped on every qualifying order; re-running is a no-op (changed should be 0).")
	}
}

func pct(n, total int64) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(n) / float64(total)
}

// fetchBatch reads up to batchSize customers with customer_id > lastID
// (lexicographic on the string form, which matches numeric order for this
// column's actual values) together with their recomputed stats, in a single
// round-trip. Read-only — no lock held beyond this one query.
func fetchBatch(ctx context.Context, db *sql.DB, lastID string) ([]customerRow, error) {
	rows, err := db.QueryContext(ctx, `
		WITH page AS (
			SELECT customer_id, customer_nb_orders, customer_total_spent, last_order_date
			FROM customer
			WHERE ($1 = '' OR customer_id > $1::integer)
			ORDER BY customer_id
			LIMIT $2
		),
		recomputed AS (
			SELECT o.customer_id,
				COUNT(*) AS nb_orders,
				COALESCE(SUM(o.price), 0) AS total_spent,
				MAX(o.creation_date) AS last_order_date
			FROM orders o
			WHERE o.customer_id IN (SELECT customer_id FROM page)
			  AND o.state IN ('CLOSED', 'DONE')
			  AND upper(o.brand_status) NOT IN ('DELETED', 'CANCELED')
			GROUP BY o.customer_id
		)
		SELECT p.customer_id::text, p.customer_nb_orders, p.customer_total_spent, p.last_order_date,
			COALESCE(r.nb_orders, 0), COALESCE(r.total_spent, 0), r.last_order_date
		FROM page p
		LEFT JOIN recomputed r ON r.customer_id = p.customer_id
		ORDER BY p.customer_id
	`, lastID, batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []customerRow
	for rows.Next() {
		var c customerRow
		if err := rows.Scan(
			&c.customerID, &c.storedNbOrders, &c.storedTotalSpent, &c.storedLastOrder,
			&c.recomputedNbOrders, &c.recomputedTotalSpent, &c.recomputedLastOrder,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// applyCustomer writes the recomputed stats for one customer and stamps
// orders.customer_stats_counted_at on that customer's orders to match —
// both in one short transaction, so `customer` is never held locked for
// longer than a single row's worth of work.
func applyCustomer(ctx context.Context, db *sql.DB, c customerRow) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE customer
		SET customer_nb_orders = $2,
			customer_total_spent = $3,
			last_order_date = $4
		WHERE customer_id = $1
	`, c.customerID, c.recomputedNbOrders, c.recomputedTotalSpent, c.recomputedLastOrder); err != nil {
		return fmt.Errorf("update customer: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE orders
		SET customer_stats_counted_at = CASE
			WHEN state IN ('CLOSED', 'DONE') AND upper(brand_status) NOT IN ('DELETED', 'CANCELED')
				THEN COALESCE(customer_stats_counted_at, now())
			ELSE NULL
		END
		WHERE customer_id = $1
	`, c.customerID); err != nil {
		return fmt.Errorf("stamp customer_stats_counted_at: %w", err)
	}

	return tx.Commit()
}
