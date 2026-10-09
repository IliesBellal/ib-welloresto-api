// Command merge_duplicate_customers finds customer rows that are true
// duplicates — same merchant_id, same normalized customer_tel, both enabled
// — and merges every loser into the oldest ("canonical") row:
//
//   - every FK table referencing the loser's customer_id is repointed to the
//     canonical id (orders, bookings, booking_waitlist,
//     customer_advertisement_emails, discount_redemptions, customer_rewards);
//   - customer_loyalty_progress rows for the same (customer, program) are
//     consolidated into one row, and customer_loyalty_progress_order ledger
//     rows are repointed/deduplicated to match;
//   - customer.customer_nb_orders/customer_total_spent/last_order_date are
//     recomputed from the canonical customer's real order history (same
//     canonical scope as cmd/backfill_customer_stats:
//     state IN ('CLOSED','DONE') AND upper(brand_status) NOT IN
//     ('DELETED','CANCELED'), all brands) — this is "summing" nb_orders/
//     total_spent the correct way: from the real merged order history,
//     rather than adding together two already-cached counters that may
//     individually have drifted;
//   - loyalty progress (current_value) for every program the group ever
//     touched is recomputed from that same real order history, scoped to
//     brand='WELLO_RESTO' and the program's own target_order_type — the
//     same rule UpdateLoyaltyFromOrder applies order-by-order
//     (internal/modules/customers/repository.go). A missing
//     customer_loyalty_progress_order ledger row is backfilled for every
//     qualifying order so a future order can never be re-counted;
//   - rewards are reconciled ADD-ONLY: if the recomputed progress implies
//     more rewards than currently exist for that customer+program, the
//     missing ones are created (unused). If it implies fewer (a customer
//     already over-credited by the pre-fix duplication bug), this is only
//     reported — an automated script must never revoke a reward a customer
//     may already have redeemed in person;
//   - loser rows are soft-deleted (enabled = false), never hard-deleted.
//
// Every group is processed in its own transaction — committed only with
// --apply, rolled back otherwise — so a dry-run report reflects exactly the
// same computation --apply would persist, not a separate simulation code
// path.
//
// Idempotent: a merged group's losers are enabled=false, so they never
// match the duplicate-finding query again on a re-run.
//
// Deliberately not using internal/config.Load()/internal/database: same
// posture as cmd/backfill_customer_stats — a standalone, DB-only
// maintenance tool that must not require GOOGLE_API_KEY, R2_PRIVATE_BUCKET,
// PIN_PEPPER.
//
// Usage:
//
//	# Always run this first — writes nothing (everything happens inside a
//	# transaction that gets rolled back), reports exactly what --apply would do.
//	POSTGRES_URL="postgres://..." go run ./cmd/merge_duplicate_customers
//
//	# Restrict to one merchant while validating the report:
//	POSTGRES_URL="postgres://..." go run ./cmd/merge_duplicate_customers --merchant=226
//
//	# Apply for real:
//	POSTGRES_URL="postgres://..." go run ./cmd/merge_duplicate_customers --apply
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	apply := flag.Bool("apply", false, "actually write changes (default: dry-run — writes happen in a transaction that is rolled back)")
	merchantFilter := flag.String("merchant", "", "restrict to one merchant_id (default: all merchants)")
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

	mode := "DRY-RUN (every write below happens in a transaction that gets rolled back)"
	if *apply {
		mode = "APPLY (writing changes for real)"
	}
	fmt.Printf("merge_duplicate_customers — database %q — %s\n", dbName, mode)
	if *merchantFilter != "" {
		fmt.Printf("restricted to merchant_id=%s\n", *merchantFilter)
	}
	fmt.Println()

	groups, err := findDuplicateGroups(ctx, db, *merchantFilter)
	if err != nil {
		log.Fatalf("find duplicate groups: %v", err)
	}
	fmt.Printf("found %d duplicate group(s)\n\n", len(groups))

	var (
		totalGroups            int
		totalLosersDisabled    int
		totalRewardsAdded      int
		totalRewardsOverIssued int
		totalLedgerBackfilled  int
		totalDangling          int
	)

	for _, g := range groups {
		rep, err := mergeOneGroup(ctx, db, g, *apply)
		if err != nil {
			log.Fatalf("merge group merchant=%s tel=%s ids=%v: %v", g.merchantID, g.tel, g.ids, err)
		}
		printReport(rep)
		totalGroups++
		totalLosersDisabled += len(g.ids) - 1
		totalRewardsAdded += rep.rewardsAdded
		totalRewardsOverIssued += rep.rewardsOverIssued
		totalLedgerBackfilled += rep.ledgerRowsBackfilled
		totalDangling += len(rep.danglingPrograms)
	}

	fmt.Println("=== SUMMARY ===")
	fmt.Printf("groups merged=%d, loser rows disabled=%d\n", totalGroups, totalLosersDisabled)
	fmt.Printf("loyalty ledger rows backfilled=%d, rewards added=%d, rewards flagged as already over-issued (not touched)=%d\n",
		totalLedgerBackfilled, totalRewardsAdded, totalRewardsOverIssued)
	if totalDangling > 0 {
		fmt.Printf("dangling loyalty_program_id references skipped (pre-existing data issue, not this tool's job)=%d\n", totalDangling)
	}
	if !*apply {
		fmt.Println("\nThis was a DRY-RUN — nothing was written. Re-run with --apply to write these changes.")
	} else {
		fmt.Println("\nApplied. Re-running should now report 0 duplicate groups.")
	}
}

// --- discovery -------------------------------------------------------

type group struct {
	merchantID string
	tel        string
	ids        []int64 // ordered oldest-first (creation_date ASC, customer_id ASC) — ids[0] is canonical
}

func findDuplicateGroups(ctx context.Context, db *sql.DB, merchantFilter string) ([]group, error) {
	// string_agg(...)::text rather than array_agg: database/sql's generic
	// Scan doesn't support Postgres arrays without pgtype — a comma-joined
	// string, split in Go, avoids pulling in that dependency for one query.
	query := `
		SELECT merchant_id, customer_tel,
		       string_agg(customer_id::text, ',' ORDER BY creation_date ASC, customer_id ASC) AS ids
		FROM customer
		WHERE enabled = true AND customer_tel IS NOT NULL AND customer_tel <> ''
		  AND ($1 = '' OR merchant_id = $1)
		GROUP BY merchant_id, customer_tel
		HAVING COUNT(*) > 1
		ORDER BY merchant_id, customer_tel
	`
	rows, err := db.QueryContext(ctx, query, merchantFilter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []group
	for rows.Next() {
		var g group
		var idsCSV string
		if err := rows.Scan(&g.merchantID, &g.tel, &idsCSV); err != nil {
			return nil, err
		}
		for _, part := range strings.Split(idsCSV, ",") {
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse customer_id %q: %w", part, err)
			}
			g.ids = append(g.ids, id)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// --- per-group merge ---------------------------------------------------

type mergeReport struct {
	merchantID     string
	tel            string
	canonicalID    int64
	loserIDs       []int64
	nbOrdersBefore map[int64]int64
	totalSpentBefore map[int64]int64
	nbOrdersAfter    int64
	totalSpentAfter  int64
	lastOrderAfter   sql.NullTime
	programs         []programReport
	danglingPrograms []string
	rewardsAdded         int
	rewardsOverIssued    int
	ledgerRowsBackfilled int
}

type programReport struct {
	programID           string
	name                string
	currentValueAfter   int64
	targetValue         int64
	rewardsExpected     int64
	rewardsActualBefore int64
	rewardsAdded        int64
	rewardsOverIssued   int64
	ledgerBackfilled    int64
}

func mergeOneGroup(ctx context.Context, db *sql.DB, g group, apply bool) (*mergeReport, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	canonical := g.ids[0]
	losers := g.ids[1:]

	rep := &mergeReport{
		merchantID:  g.merchantID,
		tel:         g.tel,
		canonicalID: canonical,
		loserIDs:    losers,
	}

	// --- snapshot "before" customer stats for the report ---
	rep.nbOrdersBefore = map[int64]int64{}
	rep.totalSpentBefore = map[int64]int64{}
	{
		rows, err := tx.QueryContext(ctx, `
			SELECT customer_id, customer_nb_orders, customer_total_spent
			FROM customer WHERE customer_id = ANY($1)
		`, int64SliceParam(g.ids))
		if err != nil {
			return nil, fmt.Errorf("snapshot customer stats: %w", err)
		}
		for rows.Next() {
			var id, nb, spent int64
			if err := rows.Scan(&id, &nb, &spent); err != nil {
				rows.Close()
				return nil, err
			}
			rep.nbOrdersBefore[id] = nb
			rep.totalSpentBefore[id] = spent
		}
		rows.Close()
	}

	loserIDsText := make([]string, len(losers))
	for i, id := range losers {
		loserIDsText[i] = strconv.FormatInt(id, 10)
	}
	canonicalText := strconv.FormatInt(canonical, 10)

	// --- 1. repoint simple FK tables (integer customer_id) ---
	// discount_redemptions.customer_id is integer in the real staging/prod
	// schema, despite docs/migration-postgres/04-schema-postgres-target.sql
	// documenting it as varchar(64) (confirmed via information_schema —
	// schema-doc drift, not touched here). Passing it as text happened to
	// work via Postgres' implicit coercion when verified against staging,
	// but that's not something to rely on — bind it as the real integer type.
	for _, table := range []string{"orders", "bookings", "discount_redemptions"} {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET customer_id = $1 WHERE customer_id = ANY($2)`, table),
			canonical, int64SliceParam(losers)); err != nil {
			return nil, fmt.Errorf("repoint %s: %w", table, err)
		}
	}

	// --- 2. repoint simple FK tables (varchar customer_id, stored as text of the integer id) ---
	for _, table := range []string{"booking_waitlist", "customer_advertisement_emails", "customer_rewards"} {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET customer_id = $1 WHERE customer_id = ANY($2)`, table),
			canonicalText, stringSliceParam(loserIDsText)); err != nil {
			return nil, fmt.Errorf("repoint %s: %w", table, err)
		}
	}

	// --- 3. consolidate customer_loyalty_progress: one row per program, repoint ledger first ---
	allIDsText := append([]string{canonicalText}, loserIDsText...)
	progRows, err := tx.QueryContext(ctx, `
		SELECT id, loyalty_program_id, current_value
		FROM customer_loyalty_progress
		WHERE customer_id = ANY($1)
		ORDER BY loyalty_program_id, id ASC
	`, stringSliceParam(allIDsText))
	if err != nil {
		return nil, fmt.Errorf("list progress rows: %w", err)
	}
	progByProgram := map[string][]string{} // program -> progress_id list (first is kept)
	for progRows.Next() {
		var id, programID string
		var cv int64
		if err := progRows.Scan(&id, &programID, &cv); err != nil {
			progRows.Close()
			return nil, err
		}
		progByProgram[programID] = append(progByProgram[programID], id)
	}
	progRows.Close()

	for programID, ids := range progByProgram {
		survivor := ids[0]
		if len(ids) > 1 {
			dead := ids[1:]
			if _, err := tx.ExecContext(ctx, `
				UPDATE customer_loyalty_progress_order SET progress_id = $1 WHERE progress_id = ANY($2)
			`, survivor, stringSliceParam(dead)); err != nil {
				return nil, fmt.Errorf("repoint ledger for program %s: %w", programID, err)
			}
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM customer_loyalty_progress WHERE id = ANY($1)
			`, stringSliceParam(dead)); err != nil {
				return nil, fmt.Errorf("delete dup progress for program %s: %w", programID, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE customer_loyalty_progress SET customer_id = $1 WHERE id = $2
		`, canonicalText, survivor); err != nil {
			return nil, fmt.Errorf("repoint survivor progress for program %s: %w", programID, err)
		}
	}

	// --- 4. dedupe ledger (order_id, loyalty_program_id), keep lowest id ---
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM customer_loyalty_progress_order a
		USING customer_loyalty_progress_order b
		WHERE a.order_id = b.order_id
		  AND a.loyalty_program_id = b.loyalty_program_id
		  AND a.id > b.id
	`); err != nil {
		return nil, fmt.Errorf("dedupe ledger: %w", err)
	}

	// --- 5. recompute customer stats from the real, now-unified order history ---
	var nbOrders, totalSpent int64
	var lastOrder sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(price), 0), MAX(creation_date)
		FROM orders
		WHERE customer_id = $1
		  AND state IN ('CLOSED', 'DONE')
		  AND upper(brand_status) NOT IN ('DELETED', 'CANCELED')
	`, canonical).Scan(&nbOrders, &totalSpent, &lastOrder); err != nil {
		return nil, fmt.Errorf("recompute customer stats: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE customer SET customer_nb_orders = $2, customer_total_spent = $3, last_order_date = $4
		WHERE customer_id = $1
	`, canonical, nbOrders, totalSpent, lastOrder); err != nil {
		return nil, fmt.Errorf("update customer stats: %w", err)
	}
	rep.nbOrdersAfter = nbOrders
	rep.totalSpentAfter = totalSpent
	rep.lastOrderAfter = lastOrder

	// --- 6. recompute loyalty progress + reconcile rewards, program by program ---
	programIDs, err := programsTouchedByGroup(ctx, tx, allIDsText)
	if err != nil {
		return nil, fmt.Errorf("list touched programs: %w", err)
	}
	for _, programID := range programIDs {
		pr, err := recomputeProgramForCustomer(ctx, tx, g.merchantID, canonical, programID)
		if err != nil {
			return nil, fmt.Errorf("recompute program %s: %w", programID, err)
		}
		if pr == nil {
			// Dangling loyalty_program_id: no row in customer_loyalty_programs
			// matches it (pre-existing data corruption unrelated to customer
			// duplication — see docs/migration-postgres/55, a varchar(30)
			// column truncated customer_loyalty_programs.id historically).
			// Not this script's job to fix; report and move on rather than
			// abort the whole customer merge over it.
			rep.danglingPrograms = append(rep.danglingPrograms, programID)
			continue
		}
		rep.programs = append(rep.programs, *pr)
		rep.rewardsAdded += int(pr.rewardsAdded)
		rep.rewardsOverIssued += int(pr.rewardsOverIssued)
		rep.ledgerRowsBackfilled += int(pr.ledgerBackfilled)
	}

	// --- 7. soft-delete the losers ---
	if _, err := tx.ExecContext(ctx, `UPDATE customer SET enabled = false WHERE customer_id = ANY($1)`,
		int64SliceParam(losers)); err != nil {
		return nil, fmt.Errorf("disable losers: %w", err)
	}

	if apply {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit: %w", err)
		}
	}
	// dry-run: deferred tx.Rollback() above handles it.

	return rep, nil
}

func programsTouchedByGroup(ctx context.Context, tx *sql.Tx, customerIDsText []string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT loyalty_program_id FROM customer_loyalty_progress WHERE customer_id = ANY($1)
		UNION
		SELECT DISTINCT loyalty_program_id FROM customer_rewards WHERE customer_id = ANY($1)
	`, stringSliceParam(customerIDsText))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, rows.Err()
}

type loyaltyProgram struct {
	id               string
	name             string
	ptype            string
	targetValue      int64
	targetOrderType  string
	rewardType       string
	rewardValue      int64
	rewardsOrderType string
}

// loadProgram returns (nil, nil) — not an error — when programID doesn't
// match any row: pre-existing dangling references (truncated
// loyalty_program_id values from before migration report 55 widened the
// column) exist in customer_loyalty_progress/customer_rewards independently
// of the customer-duplication bug this tool fixes. The caller skips and
// reports these rather than failing the whole customer merge.
func loadProgram(ctx context.Context, tx *sql.Tx, programID string) (*loyaltyProgram, error) {
	p := &loyaltyProgram{id: programID}
	err := tx.QueryRowContext(ctx, `
		SELECT name, type, target_value, target_order_type, reward_type, reward_value, rewards_order_type
		FROM customer_loyalty_programs WHERE id = $1
	`, programID).Scan(&p.name, &p.ptype, &p.targetValue, &p.targetOrderType, &p.rewardType, &p.rewardValue, &p.rewardsOrderType)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

type qualifyingOrder struct {
	orderID   int64
	price     int64
	orderType string
}

// recomputeProgramForCustomer rebuilds current_value for one
// (canonical customer, program) pair from the real order history — the
// same per-order rule UpdateLoyaltyFromOrder applies
// (internal/modules/customers/repository.go), replayed here instead of
// trusted from whatever the duplicated rows had accumulated. It also
// backfills any missing customer_loyalty_progress_order ledger row (so a
// future order is never re-counted) and adds any reward the recomputed
// progress implies but doesn't yet exist — never removing one.
func recomputeProgramForCustomer(ctx context.Context, tx *sql.Tx, merchantID string, canonical int64, programID string) (*programReport, error) {
	p, err := loadProgram(ctx, tx, programID)
	if err != nil {
		return nil, fmt.Errorf("load program: %w", err)
	}
	if p == nil {
		return nil, nil
	}
	allowedTypes := parseOrderTypes(p.targetOrderType)

	rows, err := tx.QueryContext(ctx, `
		SELECT order_id, price, COALESCE(order_type, '')
		FROM orders
		WHERE customer_id = $1 AND merchant_id = $2 AND brand = 'WELLO_RESTO'
		  AND state IN ('CLOSED', 'DONE') AND upper(brand_status) NOT IN ('DELETED', 'CANCELED')
		ORDER BY order_id
	`, canonical, merchantID)
	if err != nil {
		return nil, fmt.Errorf("list qualifying orders: %w", err)
	}
	var orders []qualifyingOrder
	for rows.Next() {
		var o qualifyingOrder
		if err := rows.Scan(&o.orderID, &o.price, &o.orderType); err != nil {
			rows.Close()
			return nil, err
		}
		orders = append(orders, o)
	}
	rows.Close()

	// Filter by the program's own target_order_type, exactly like isOrderTypeAllowed.
	var filtered []qualifyingOrder
	for _, o := range orders {
		if orderTypeAllowed(allowedTypes, o.orderType) {
			filtered = append(filtered, o)
		}
	}

	var perOrderQty map[int64]int64
	if p.ptype == "product_count" || p.ptype == "products_count" {
		perOrderQty, err = productCountPerOrder(ctx, tx, programID, filtered)
		if err != nil {
			return nil, fmt.Errorf("product count: %w", err)
		}
	}

	var total int64
	increments := map[int64]int64{} // order_id -> increment for this program
	for _, o := range filtered {
		var inc int64
		switch p.ptype {
		case "orders_count":
			inc = 1
		case "total_spent":
			inc = o.price
		case "product_count", "products_count":
			inc = perOrderQty[o.orderID]
		}
		if inc == 0 {
			continue
		}
		increments[o.orderID] = inc
		total += inc
	}

	// Ensure/replace the single surviving progress row for this customer+program.
	var progressID string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM customer_loyalty_progress WHERE customer_id = $1 AND loyalty_program_id = $2
	`, strconv.FormatInt(canonical, 10), programID).Scan(&progressID)
	if err == sql.ErrNoRows {
		progressID = fmt.Sprintf("cus-progress-merge-%d-%s", canonical, programID)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO customer_loyalty_progress (id, customer_id, loyalty_program_id, current_value, last_update)
			VALUES ($1, $2, $3, $4, now())
		`, progressID, strconv.FormatInt(canonical, 10), programID, total); err != nil {
			return nil, fmt.Errorf("insert progress: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("lookup progress: %w", err)
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE customer_loyalty_progress SET current_value = $1, last_update = now() WHERE id = $2
		`, total, progressID); err != nil {
			return nil, fmt.Errorf("update progress: %w", err)
		}
	}

	// Backfill any missing ledger row for a qualifying order.
	var backfilled int64
	for orderID, inc := range increments {
		var exists int
		err := tx.QueryRowContext(ctx, `
			SELECT 1 FROM customer_loyalty_progress_order WHERE order_id = $1 AND loyalty_program_id = $2
		`, orderID, programID).Scan(&exists)
		if err == nil {
			continue
		}
		if err != sql.ErrNoRows {
			return nil, fmt.Errorf("check ledger for order %d: %w", orderID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO customer_loyalty_progress_order (loyalty_program_id, progress_id, order_id, increment_value)
			VALUES ($1, $2, $3, $4)
		`, programID, progressID, orderID, inc); err != nil {
			return nil, fmt.Errorf("backfill ledger for order %d: %w", orderID, err)
		}
		backfilled++
	}

	// Reconcile rewards: add-only.
	var expected int64
	if p.targetValue > 0 {
		expected = total / p.targetValue
	}
	var actual int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM customer_rewards WHERE customer_id = $1 AND loyalty_program_id = $2
	`, strconv.FormatInt(canonical, 10), programID).Scan(&actual); err != nil {
		return nil, fmt.Errorf("count existing rewards: %w", err)
	}

	pr := &programReport{
		programID:           programID,
		name:                p.name,
		currentValueAfter:   total,
		targetValue:         p.targetValue,
		rewardsExpected:     expected,
		rewardsActualBefore: actual,
		ledgerBackfilled:    backfilled,
	}

	if expected > actual {
		toAdd := expected - actual
		for i := int64(0); i < toAdd; i++ {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO customer_rewards (customer_id, loyalty_program_id, reward_type, reward_order_type, reward_value, creation_date)
				VALUES ($1, $2, $3, $4, $5, now())
			`, strconv.FormatInt(canonical, 10), programID, p.rewardType, p.rewardsOrderType, p.rewardValue); err != nil {
				return nil, fmt.Errorf("add missing reward: %w", err)
			}
		}
		pr.rewardsAdded = toAdd
	} else if expected < actual {
		pr.rewardsOverIssued = actual - expected
	}

	return pr, nil
}

// --- small helpers (deliberately reimplemented, not imported from
// internal/modules/customers, to keep this standalone tool dependency-free —
// see package doc) ---

func parseOrderTypes(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		var arr []string
		if err := json.Unmarshal([]byte(raw), &arr); err == nil {
			clean := make([]string, 0, len(arr))
			for _, item := range arr {
				item = strings.TrimSpace(item)
				if item != "" {
					clean = append(clean, strings.ToUpper(item))
				}
			}
			return clean
		}
	}
	fields := strings.Fields(strings.ReplaceAll(raw, ",", " "))
	clean := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" {
			clean = append(clean, strings.ToUpper(f))
		}
	}
	return clean
}

func orderTypeAllowed(allowed []string, orderType string) bool {
	if len(allowed) == 0 {
		return true
	}
	ot := strings.ToUpper(strings.TrimSpace(orderType))
	for _, a := range allowed {
		if a == ot {
			return true
		}
	}
	return false
}

func productCountPerOrder(ctx context.Context, tx *sql.Tx, programID string, orders []qualifyingOrder) (map[int64]int64, error) {
	out := map[int64]int64{}
	if len(orders) == 0 {
		return out, nil
	}
	ids := make([]int64, len(orders))
	for i, o := range orders {
		ids[i] = o.orderID
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT oi.order_id, COALESCE(SUM(oi.quantity), 0)
		FROM orderitems oi
		INNER JOIN customer_loyalty_program_target_products tp
			ON tp.product_id = CAST(oi.product_id AS TEXT) AND tp.loyalty_program_id = $1
		WHERE oi.order_id = ANY($2)
		GROUP BY oi.order_id
	`, programID, int64SliceParam(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, qty int64
		if err := rows.Scan(&id, &qty); err != nil {
			return nil, err
		}
		out[id] = qty
	}
	return out, rows.Err()
}

// --- pgx array params ---

func int64SliceParam(ids []int64) []int64 { return ids }
func stringSliceParam(ids []string) []string { return ids }

// --- reporting ---

func printReport(r *mergeReport) {
	fmt.Printf("merchant=%s tel=%s canonical=%d losers=%v\n", r.merchantID, r.tel, r.canonicalID, r.loserIDs)
	var nbBefore, spentBefore int64
	for _, id := range append([]int64{r.canonicalID}, r.loserIDs...) {
		nbBefore += r.nbOrdersBefore[id]
		spentBefore += r.totalSpentBefore[id]
	}
	fmt.Printf("  customer stats: nb_orders %d (sum of cached counters) -> %d (recomputed from real orders), total_spent %d -> %d centimes\n",
		nbBefore, r.nbOrdersAfter, spentBefore, r.totalSpentAfter)
	if r.lastOrderAfter.Valid {
		fmt.Printf("  last_order_date -> %s\n", r.lastOrderAfter.Time.Format(time.RFC3339))
	}
	for _, pr := range r.programs {
		fmt.Printf("  program %s (%s): current_value recomputed=%d, target=%d, rewards expected=%d actual_before=%d added=%d over_issued_flagged=%d\n",
			pr.programID, pr.name, pr.currentValueAfter, pr.targetValue, pr.rewardsExpected, pr.rewardsActualBefore, pr.rewardsAdded, pr.rewardsOverIssued)
	}
	for _, dangling := range r.danglingPrograms {
		fmt.Printf("  WARNING: loyalty_program_id=%q referenced by this customer has no matching row in customer_loyalty_programs (pre-existing dangling reference, not touched)\n", dangling)
	}
	fmt.Println()
}
