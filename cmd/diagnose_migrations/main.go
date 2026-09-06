// Command diagnose_migrations is a read-only, standalone audit of which
// migrations under migrations/todo/ (087, 094-117) actually took effect
// against a Postgres database — staging or production.
//
// Why this exists: this project applies migrations by hand and keeps no
// migration-tracking table (see CLAUDE.md, "no migration tool"), so there is
// no authoritative record anywhere of what has actually run. PROMPT 13
// (docs/migration-postgres/, RBAC/analytics work, 2026-09) needed to
// reconstruct that record from the schema itself, migration by migration,
// because a prior session's guess ("094-111 must all be behind us by now")
// turned out to be wrong for at least three of them. This tool is the
// reusable form of that reconstruction — see
// docs/migration-postgres/67-migration-status-audit.md for the staging
// result this tool produced, the resulting apply plan, and the
// schema_migrations tracking proposal that would make re-running this by
// hand unnecessary in the future.
//
// Safety: every check is a SELECT. The whole run happens inside one
// `BEGIN ... READ ONLY` transaction that is always rolled back at the end
// (never committed) — even a bug that tried to sneak in a write would be
// rejected by Postgres itself ("cannot execute ... in a read-only
// transaction"), not merely by this program's good behavior. Nothing here
// creates, drops, or updates anything, on staging or on production.
//
// Deliberately NOT using internal/config.Load() / internal/database: those
// pull in unrelated required env vars (GOOGLE_API_KEY, R2_PRIVATE_BUCKET,
// PIN_PEPPER) that have nothing to do with a schema-only, read-only check,
// and would tempt whoever runs this against production into typing real
// unrelated secrets for values the tool never uses. This connects directly
// with database/sql + pgx, same pattern as any other throwaway diagnostic
// script against this project's databases.
//
// Usage:
//
//	POSTGRES_URL="postgres://...production-or-staging-connection-string..." \
//	  go run ./cmd/diagnose_migrations
//
// Exit code is always 0 once connected — this tool reports, it never fails
// the build/pipeline based on what it finds. A connection error exits 1.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Verdict is one of exactly three states, per PROMPT 13's method: a migration
// is APPLIQUÉE, NON_APPLIQUÉE, or — when neither can be shown from the
// schema alone (an idempotent statement with no observable trace, or a
// dataset too small/empty to distinguish the two states) — INDÉTERMINABLE.
// PARTIELLE is a fourth, deliberately louder state for the single most
// dangerous case the brief calls out: a migration with several statements,
// some of which ran and some of which didn't.
type Verdict string

const (
	Applied       Verdict = "APPLIQUÉE"
	NotApplied    Verdict = "NON APPLIQUÉE"
	Partial       Verdict = "PARTIELLE"
	Undeterminate Verdict = "INDÉTERMINABLE"
)

type result struct {
	ID      string
	Name    string
	Verdict Verdict
	Detail  string
}

func main() {
	url := os.Getenv("POSTGRES_URL")
	if url == "" {
		log.Fatal("POSTGRES_URL is not set — point it at the database to audit (staging or production) and re-run")
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

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		log.Fatalf("begin read-only transaction: %v", err)
	}
	// Always roll back — this transaction never has anything to commit.
	defer tx.Rollback()

	var dbName string
	_ = tx.QueryRowContext(ctx, `SELECT current_database()`).Scan(&dbName)
	fmt.Printf("Diagnostic migrations — connected to %q (read-only transaction)\n", dbName)
	fmt.Println(strings.Repeat("=", 78))

	results := []result{
		check087(ctx, tx),
		check094(ctx, tx),
		check095(ctx, tx),
		check096(ctx, tx),
		check097(ctx, tx),
		check098(ctx, tx),
		check099(ctx, tx),
		check100(ctx, tx),
		check101(ctx, tx),
		check102(ctx, tx),
		check103a(ctx, tx),
		check103b(ctx, tx),
		check104(ctx, tx),
		check105(ctx, tx),
		check106(ctx, tx),
		check107(ctx, tx),
		check108(ctx, tx),
		check109(ctx, tx),
		check110(ctx, tx),
		check111(ctx, tx),
		check112Informational(ctx, tx),
		check113Informational(ctx, tx),
		check114(ctx, tx),
		check115(ctx, tx),
		check116(ctx, tx),
		check117Informational(ctx, tx),
	}

	for _, r := range results {
		fmt.Printf("%-8s %-58s %-16s %s\n", r.ID, r.Name, r.Verdict, r.Detail)
	}

	fmt.Println(strings.Repeat("=", 78))
	fmt.Println("112, 113, 117 are reported for information only — they are prepared but")
	fmt.Println("deliberately not meant to be applied yet (see their migration files and")
	fmt.Println("docs/migration-postgres/67-migration-status-audit.md). Do not add them to")
	fmt.Println("an apply plan on the strength of this report alone.")
}

// --- helpers ---------------------------------------------------------------

func columnExists(ctx context.Context, tx *sql.Tx, table, column string) bool {
	var n int
	err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name=$1 AND column_name=$2`, table, column).Scan(&n)
	return err == nil && n > 0
}

func tableExists(ctx context.Context, tx *sql.Tx, table string) bool {
	var n int
	err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_name=$1`, table).Scan(&n)
	return err == nil && n > 0
}

func indexExists(ctx context.Context, tx *sql.Tx, indexName string) (exists bool, valid bool) {
	err := tx.QueryRowContext(ctx, `
		SELECT i.indisvalid FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
		WHERE c.relname = $1`, indexName).Scan(&valid)
	return err == nil, valid
}

func permissionKeyExists(ctx context.Context, tx *sql.Tx, key string) bool {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM permissions WHERE key=$1`, key).Scan(&n)
	return err == nil && n > 0
}

func columnWidth(ctx context.Context, tx *sql.Tx, table, column string) (int, bool) {
	var n sql.NullInt64
	err := tx.QueryRowContext(ctx, `
		SELECT character_maximum_length FROM information_schema.columns
		WHERE table_schema='public' AND table_name=$1 AND column_name=$2`, table, column).Scan(&n)
	if err != nil || !n.Valid {
		return 0, false
	}
	return int(n.Int64), true
}

// --- checks, one per migration ---------------------------------------------

func check087(ctx context.Context, tx *sql.Tx) result {
	names := []string{"idx_orders_merchant_creation", "idx_orderitems_order_id", "idx_extra_order_item_id", "idx_payments_order_id"}
	found, valid := 0, 0
	for _, n := range names {
		exists, isValid := indexExists(ctx, tx, n)
		if exists {
			found++
			if isValid {
				valid++
			}
		}
	}
	detail := fmt.Sprintf("%d/4 index(es) present, %d/4 valid", found, valid)
	switch {
	case found == 0:
		return result{"087", "analytics_indexes (4 index CONCURRENTLY)", NotApplied, detail}
	case found == 4 && valid == 4:
		return result{"087", "analytics_indexes (4 index CONCURRENTLY)", Applied, detail}
	default:
		return result{"087", "analytics_indexes (4 index CONCURRENTLY)", Partial, detail + " — some index(es) missing or left invalid, see migration's own down.sql before replaying"}
	}
}

func check094(ctx context.Context, tx *sql.Tx) result {
	tablesOK := tableExists(ctx, tx, "permissions") && tableExists(ctx, tx, "roles") && tableExists(ctx, tx, "role_permissions")
	colsOK := columnExists(ctx, tx, "users_rights", "role_id") && columnExists(ctx, tx, "merchant", "default_role_id")
	w1, ok1 := columnWidth(ctx, tx, "audit_logs", "resource_id")
	w2, ok2 := columnWidth(ctx, tx, "audit_logs", "user_id")
	widthOK := ok1 && ok2 && w1 >= 64 && w2 >= 64
	detail := fmt.Sprintf("tables=%v cols=%v audit_logs widths resource_id=%d user_id=%d", tablesOK, colsOK, w1, w2)
	switch {
	case tablesOK && colsOK && widthOK:
		return result{"094", "roles_schema (RBAC lot 1, ran as 089 on staging)", Applied, detail}
	case !tablesOK && !colsOK && !widthOK:
		return result{"094", "roles_schema (RBAC lot 1, ran as 089 on staging)", NotApplied, detail}
	default:
		return result{"094", "roles_schema (RBAC lot 1, ran as 089 on staging)", Partial, detail}
	}
}

func check095(ctx context.Context, tx *sql.Tx) result {
	// catalog.manage is never touched by any later migration (100 only
	// removes pos.access/pos.discount.apply) — a stable marker for lot 1.
	ok := permissionKeyExists(ctx, tx, "catalog.manage") && permissionKeyExists(ctx, tx, "inventory.manage")
	if ok {
		return result{"095", "roles_permissions_catalog (14 initial keys)", Applied, "catalog.manage / inventory.manage present"}
	}
	return result{"095", "roles_permissions_catalog (14 initial keys)", NotApplied, "catalog.manage absent"}
}

func check096(ctx context.Context, tx *sql.Tx) result {
	// This migration is a no-op placeholder; the real effect is
	// cmd/seed_system_roles having been run. Not a one-time schema change —
	// re-running it is the supported way to backfill new merchants, so
	// "applied" here means "has ever been run at least once", not "is
	// permanently and fully done for all time".
	var adminRoles, merchants int
	_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM roles WHERE system_key='admin'`).Scan(&adminRoles)
	_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM merchant`).Scan(&merchants)
	detail := fmt.Sprintf("%d admin role(s) seeded / %d merchant(s) total", adminRoles, merchants)
	switch {
	case adminRoles == 0:
		return result{"096", "seed_system_roles (cmd/seed_system_roles, not raw SQL)", NotApplied, detail}
	case adminRoles < merchants:
		return result{"096", "seed_system_roles (cmd/seed_system_roles, not raw SQL)", Partial, detail + " — re-run for merchants missing an admin role"}
	default:
		return result{"096", "seed_system_roles (cmd/seed_system_roles, not raw SQL)", Applied, detail}
	}
}

func check097(ctx context.Context, tx *sql.Tx) result {
	if permissionKeyExists(ctx, tx, "pos.status.manage") {
		return result{"097", "permission_pos_status_manage", Applied, "pos.status.manage present"}
	}
	return result{"097", "permission_pos_status_manage", NotApplied, "pos.status.manage absent"}
}

func check098(ctx context.Context, tx *sql.Tx) result {
	if !tableExists(ctx, tx, "access_observation") {
		return result{"098", "access_observation table", NotApplied, "table absent"}
	}
	var n int
	_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM access_observation`).Scan(&n)
	return result{"098", "access_observation table", Applied, fmt.Sprintf("table present, %d row(s) observed so far", n)}
}

func check099(ctx context.Context, tx *sql.Tx) result {
	var withAdmin, pointingAtAdmin int
	err1 := tx.QueryRowContext(ctx, `
		SELECT count(DISTINCT r.merchant_id) FROM roles r WHERE r.system_key='admin'`).Scan(&withAdmin)
	err2 := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM merchant m JOIN roles r ON r.id = m.default_role_id
		WHERE r.system_key = 'admin'`).Scan(&pointingAtAdmin)
	if err1 != nil || err2 != nil {
		return result{"099", "merchant_default_role_admin", Undeterminate, "query failed (missing merchant/roles data?)"}
	}
	detail := fmt.Sprintf("%d/%d merchant(s) with an admin role point default_role_id at it", pointingAtAdmin, withAdmin)
	switch {
	case withAdmin == 0:
		return result{"099", "merchant_default_role_admin", Undeterminate, "no merchant has an admin role yet (096 not run) — cannot observe 099's effect"}
	case pointingAtAdmin == withAdmin:
		return result{"099", "merchant_default_role_admin", Applied, detail}
	case pointingAtAdmin == 0:
		return result{"099", "merchant_default_role_admin", NotApplied, detail}
	default:
		return result{"099", "merchant_default_role_admin", Partial, detail}
	}
}

func check100(ctx context.Context, tx *sql.Tx) result {
	a := permissionKeyExists(ctx, tx, "pos.access")
	b := permissionKeyExists(ctx, tx, "pos.discount.apply")
	if !a && !b {
		return result{"100", "deprecate_pos_access_and_discount_apply", Applied, "both keys absent"}
	}
	if a && b {
		return result{"100", "deprecate_pos_access_and_discount_apply", NotApplied, "both keys still present"}
	}
	return result{"100", "deprecate_pos_access_and_discount_apply", Partial, fmt.Sprintf("pos.access present=%v, pos.discount.apply present=%v", a, b)}
}

func check101(ctx context.Context, tx *sql.Tx) result {
	a := tableExists(ctx, tx, "production_profiles")
	b := tableExists(ctx, tx, "product_production_profiles")
	note := " — NOTE: the .up.sql file on disk is written in MySQL syntax (ENGINE=InnoDB, INT UNSIGNED, AFTER) and would error verbatim against Postgres; whatever created these tables was not this file as-is"
	switch {
	case a && b:
		return result{"101", "production_profiles", Applied, "both tables present" + note}
	case !a && !b:
		return result{"101", "production_profiles", NotApplied, "neither table present"}
	default:
		return result{"101", "production_profiles", Partial, fmt.Sprintf("production_profiles=%v product_production_profiles=%v", a, b)}
	}
}

func check102(ctx context.Context, tx *sql.Tx) result {
	col := columnExists(ctx, tx, "orders", "delivery_travel_seconds")
	tbl := tableExists(ctx, tx, "average_delivery_time")
	note := " — NOTE: the .up.sql file on disk uses MySQL-only syntax (COMMENT '...', AFTER); would error verbatim against Postgres"
	switch {
	case col && tbl:
		return result{"102", "delivery_travel_seconds", Applied, "column + table present" + note}
	case !col && !tbl:
		return result{"102", "delivery_travel_seconds", NotApplied, "column and table absent"}
	default:
		return result{"102", "delivery_travel_seconds", Partial, fmt.Sprintf("orders.delivery_travel_seconds=%v average_delivery_time table=%v", col, tbl)}
	}
}

func check103a(ctx context.Context, tx *sql.Tx) result {
	keys := []string{"pos.analytics", "bookings.manage", "platforms.manage", "kiosk.manage", "seating_plan.manage"}
	found := 0
	for _, k := range keys {
		if permissionKeyExists(ctx, tx, k) {
			found++
		}
	}
	var emptyDesc int
	_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM permissions WHERE description = ''`).Scan(&emptyDesc)
	detail := fmt.Sprintf("%d/5 new key(s) present, %d permission(s) with empty description", found, emptyDesc)
	switch {
	case found == 5 && emptyDesc == 0:
		return result{"103a", "permission_catalog_lot10 (second '103' file)", Applied, detail}
	case found == 0:
		return result{"103a", "permission_catalog_lot10 (second '103' file)", NotApplied, detail}
	default:
		return result{"103a", "permission_catalog_lot10 (second '103' file)", Partial, detail}
	}
}

func check103b(ctx context.Context, tx *sql.Tx) result {
	a := columnExists(ctx, tx, "orders", "production_ready_at")
	b := columnExists(ctx, tx, "orders", "delivery_arrival_at")
	dropped := !columnExists(ctx, tx, "orders", "datecall")
	detail := fmt.Sprintf("production_ready_at=%v delivery_arrival_at=%v dateCall dropped=%v", a, b, dropped)
	switch {
	case a && b && dropped:
		return result{"103b", "production_ready_delivery_arrival (first '103' file)", Applied, detail}
	case !a && !b && !dropped:
		return result{"103b", "production_ready_delivery_arrival (first '103' file)", NotApplied, detail}
	default:
		return result{"103b", "production_ready_delivery_arrival (first '103' file)", Partial, detail}
	}
}

func check104(ctx context.Context, tx *sql.Tx) result {
	dropped := []bool{
		!columnExists(ctx, tx, "employees", "role"),
		!columnExists(ctx, tx, "employees", "job_title"),
		!columnExists(ctx, tx, "users_rights", "role"),
		!columnExists(ctx, tx, "users_rights", "job_title"),
		!columnExists(ctx, tx, "planning_shifts", "title"),
		!columnExists(ctx, tx, "planning_shifts", "location"),
		!columnExists(ctx, tx, "planning_published_shift_snapshots", "title"),
	}
	count := 0
	for _, d := range dropped {
		if d {
			count++
		}
	}
	detail := fmt.Sprintf("%d/7 target column(s) dropped", count)
	switch {
	case count == 7:
		return result{"104", "drop_role_job_title_shift_title_location", Applied, detail}
	case count == 0:
		return result{"104", "drop_role_job_title_shift_title_location", NotApplied, detail}
	default:
		return result{"104", "drop_role_job_title_shift_title_location", Partial, detail}
	}
}

func check105(ctx context.Context, tx *sql.Tx) result {
	var n int
	err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_enum e JOIN pg_type t ON t.oid = e.enumtypid
		WHERE t.typname = 'planning_shifts_status_enum' AND e.enumlabel = 'published'`).Scan(&n)
	if err == nil && n > 0 {
		return result{"105", "add_published_shift_status (enum value)", Applied, "'published' present in planning_shifts_status_enum"}
	}
	return result{"105", "add_published_shift_status (enum value)", NotApplied, "'published' absent from planning_shifts_status_enum"}
}

func check106(ctx context.Context, tx *sql.Tx) result {
	if !tableExists(ctx, tx, "planning_shifts") {
		return result{"106", "backfill_shift_status_to_published", Undeterminate, "planning_shifts table absent"}
	}
	var total, published, oldStates int
	_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM planning_shifts`).Scan(&total)
	if total == 0 {
		return result{"106", "backfill_shift_status_to_published", Undeterminate, "planning_shifts has 0 rows — nothing to observe either way"}
	}
	_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM planning_shifts WHERE status::text = 'published'`).Scan(&published)
	_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM planning_shifts WHERE status::text IN ('planned','confirmed','done','cancelled')`).Scan(&oldStates)
	detail := fmt.Sprintf("%d published / %d in a pre-migration state / %d total", published, oldStates, total)
	switch {
	case oldStates == 0 && published > 0:
		return result{"106", "backfill_shift_status_to_published", Applied, detail}
	case published == 0:
		return result{"106", "backfill_shift_status_to_published", NotApplied, detail}
	default:
		return result{"106", "backfill_shift_status_to_published", Partial, detail}
	}
}

func check107(ctx context.Context, tx *sql.Tx) result {
	a := tableExists(ctx, tx, "import_component_categories_mapping")
	b := tableExists(ctx, tx, "import_components_mapping")
	switch {
	case a && b:
		return result{"107", "import_component_mappings", Applied, "both tables present"}
	case !a && !b:
		return result{"107", "import_component_mappings", NotApplied, "neither table present"}
	default:
		return result{"107", "import_component_mappings", Partial, fmt.Sprintf("import_component_categories_mapping=%v import_components_mapping=%v", a, b)}
	}
}

func check108(ctx context.Context, tx *sql.Tx) result {
	if columnExists(ctx, tx, "api_request_logs", "response_payload") {
		return result{"108", "api_request_logs_response_payload", Applied, "column present"}
	}
	return result{"108", "api_request_logs_response_payload", NotApplied, "column absent"}
}

func check109(ctx context.Context, tx *sql.Tx) result {
	exists, valid := indexExists(ctx, tx, "idx_api_request_logs_created_at")
	if exists && valid {
		return result{"109", "api_request_logs_created_at_index", Applied, "index present and valid"}
	}
	if exists && !valid {
		return result{"109", "api_request_logs_created_at_index", Partial, "index present but INVALID — drop and recreate, do not leave in place"}
	}
	return result{"109", "api_request_logs_created_at_index", NotApplied, "index absent"}
}

func check110(ctx context.Context, tx *sql.Tx) result {
	cols := []string{"access_wrdelivery", "access_wrwaiter", "export_reports", "export_financials", "export_customers"}
	present := 0
	for _, c := range cols {
		if columnExists(ctx, tx, "users_rights", c) {
			present++
		}
	}
	detail := fmt.Sprintf("%d/5 target column(s) still present", present)
	switch {
	case present == 0:
		return result{"110", "drop_dead_legacy_rights_columns", Applied, detail}
	case present == 5:
		return result{"110", "drop_dead_legacy_rights_columns", NotApplied, detail}
	default:
		return result{"110", "drop_dead_legacy_rights_columns", Partial, detail}
	}
}

func check111(ctx context.Context, tx *sql.Tx) result {
	pkCols := func(table string) int {
		var n int
		_ = tx.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu ON kcu.constraint_name = tc.constraint_name
			WHERE tc.constraint_type='PRIMARY KEY' AND tc.table_name=$1`, table).Scan(&n)
		return n
	}
	ueCols := pkCols("integration_uber_eats")
	drCols := pkCols("integration_deliveroo")
	brandStoreID := columnExists(ctx, tx, "orders", "brand_store_id")
	detail := fmt.Sprintf("integration_uber_eats PK cols=%d integration_deliveroo PK cols=%d orders.brand_store_id=%v", ueCols, drCols, brandStoreID)
	switch {
	case ueCols == 2 && drCols == 2 && brandStoreID:
		return result{"111", "multi_account_uber_deliveroo", Applied, detail}
	case ueCols <= 1 && drCols <= 1 && !brandStoreID:
		return result{"111", "multi_account_uber_deliveroo", NotApplied, detail}
	default:
		return result{"111", "multi_account_uber_deliveroo", Partial, detail}
	}
}

func check112Informational(ctx context.Context, tx *sql.Tx) result {
	var n int
	_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_extension WHERE extname='pg_stat_statements'`).Scan(&n)
	if n > 0 {
		return result{"112*", "pg_stat_statements (deliberately NOT in the plan)", Applied, "extension installed"}
	}
	return result{"112*", "pg_stat_statements (deliberately NOT in the plan)", NotApplied, "extension not installed — requires a shared_preload_libraries change + restart, see the migration file"}
}

func check113Informational(ctx context.Context, tx *sql.Tx) result {
	if columnExists(ctx, tx, "users_rights", "admin") {
		return result{"113*", "drop_users_rights_admin (deliberately NOT in the plan)", NotApplied, "column still present — expected, blocked on a code deploy, see the migration file"}
	}
	return result{"113*", "drop_users_rights_admin (deliberately NOT in the plan)", Applied, "column absent"}
}

func check114(ctx context.Context, tx *sql.Tx) result {
	cols := []bool{
		columnExists(ctx, tx, "orderitems", "cost_price_unit"),
		columnExists(ctx, tx, "orderitems", "cost_price_reason"),
		columnExists(ctx, tx, "orders", "order_source"),
		columnExists(ctx, tx, "orders", "cancelled_by_type"),
		columnExists(ctx, tx, "customer", "acquisition_source"),
	}
	present := 0
	for _, c := range cols {
		if c {
			present++
		}
	}
	var lowercaseBrandStatus int
	_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM orders WHERE brand_status <> upper(brand_status)`).Scan(&lowercaseBrandStatus)
	var withSource, totalOrders int
	_ = tx.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE order_source IS NOT NULL), count(*) FROM orders`).Scan(&withSource, &totalOrders)
	detail := fmt.Sprintf("%d/5 column(s) present, %d row(s) with non-uppercase brand_status, order_source backfilled %d/%d", present, lowercaseBrandStatus, withSource, totalOrders)
	switch {
	case present == 5 && lowercaseBrandStatus == 0:
		return result{"114", "write_path_instrumentation (lot 1)", Applied, detail}
	case present == 0:
		return result{"114", "write_path_instrumentation (lot 1)", NotApplied, detail}
	default:
		return result{"114", "write_path_instrumentation (lot 1)", Partial, detail}
	}
}

func check115(ctx context.Context, tx *sql.Tx) result {
	if permissionKeyExists(ctx, tx, "reports.staff_performance.read") {
		return result{"115", "permission_reports_staff_performance_read", Applied, "key present"}
	}
	return result{"115", "permission_reports_staff_performance_read", NotApplied, "key absent"}
}

func check116(ctx context.Context, tx *sql.Tx) result {
	cols := []bool{
		columnExists(ctx, tx, "order_item_configuration", "cost_price_unit"),
		columnExists(ctx, tx, "order_item_configuration", "cost_price_reason"),
		columnExists(ctx, tx, "extra", "cost_price_unit"),
		columnExists(ctx, tx, "extra", "cost_price_reason"),
	}
	present := 0
	for _, c := range cols {
		if c {
			present++
		}
	}
	w, ok := columnWidth(ctx, tx, "orders", "deletion_reason_id")
	widened := ok && w >= 32
	detail := fmt.Sprintf("%d/4 column(s) present, orders.deletion_reason_id width=%d", present, w)
	switch {
	case present == 4 && widened:
		return result{"116", "write_path_instrumentation_lot2", Applied, detail}
	case present == 0 && !widened:
		return result{"116", "write_path_instrumentation_lot2", NotApplied, detail}
	default:
		return result{"116", "write_path_instrumentation_lot2", Partial, detail}
	}
}

func check117Informational(ctx context.Context, tx *sql.Tx) result {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM orders WHERE deletion_reason_id LIKE '''%'''`).Scan(&n)
	if err != nil {
		return result{"117*", "cleanup_deletion_reason_id_quotes (deliberately NOT in the plan)", Undeterminate, "query failed"}
	}
	if n == 0 {
		return result{"117*", "cleanup_deletion_reason_id_quotes (deliberately NOT in the plan)", Applied, "0 quoted value(s) remaining"}
	}
	return result{"117*", "cleanup_deletion_reason_id_quotes (deliberately NOT in the plan)", NotApplied, fmt.Sprintf("%d quoted value(s) remaining", n)}
}
