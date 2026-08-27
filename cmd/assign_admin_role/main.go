// Command assign_admin_role is the RBAC lot 4 one-shot backfill: for every
// users_rights row with role_id IS NULL, sets role_id to that same
// merchant's "admin" role (roles.system_key = 'admin', matched on the exact
// same merchant_id — never a different establishment's admin role).
//
// Why a Go program and not plain SQL: UPDATE ... FROM (Postgres) and
// UPDATE ... JOIN (MySQL) diverge, and the repo is mid-migration between the
// two dialects (DB_DIALECT) — same rationale as cmd/seed_system_roles. Going
// through dbx.GetDB/dbx.Rebind means one implementation works under both.
//
// Processes every row, enabled or not: a disabled row grants nothing
// (auth.GetUserByToken filters on it — see internal/modules/auth/repository.go),
// but leaving role_id NULL would create a dormant inconsistency the day the
// row is re-enabled.
//
// An establishment with no "admin" role (cmd/seed_system_roles never ran for
// it) is left untouched and reported, never created on the fly here — that
// would silently paper over a seed_system_roles run that never happened.
//
// Idempotent: once a merchant's users_rights rows all have role_id set, a
// second run finds nothing left to do for it (role_id IS NULL matches
// nothing), so re-running touches zero rows.
//
// Usage:
//
//	DB_DIALECT=postgres POSTGRES_URL=... go run ./cmd/assign_admin_role --dry-run
//	DB_DIALECT=postgres POSTGRES_URL=... go run ./cmd/assign_admin_role --apply
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"

	"welloresto-api/internal/config"
	"welloresto-api/internal/database"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/utils/dbutils"
)

type merchantCount struct {
	merchantID string
	count      int64
}

func main() {
	dryRun := flag.Bool("dry-run", false, "report what would change, without writing anything")
	apply := flag.Bool("apply", false, "execute the assignment inside a transaction")
	flag.Parse()

	if *dryRun == *apply {
		log.Fatal("pass exactly one of --dry-run or --apply")
	}

	cfg := config.Load()

	var db *sql.DB
	var err error
	if dbx.ActiveDialect() == dbx.Postgres {
		db, err = database.NewPostgres(cfg.Database)
	} else {
		db, err = database.NewMySQL(cfg.Database)
	}
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	if *dryRun {
		runDryRun(ctx, db)
		return
	}
	runApply(ctx, db)
}

// eligibleCounts returns, per merchant that HAS an admin role, how many
// users_rights rows currently have role_id IS NULL for that merchant.
func eligibleCounts(ctx context.Context, q dbutils.DBTX) ([]merchantCount, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT ur.merchant_id, COUNT(*)
		FROM users_rights ur
		INNER JOIN roles r ON r.merchant_id = ur.merchant_id AND r.system_key = 'admin'
		WHERE ur.role_id IS NULL
		GROUP BY ur.merchant_id
		ORDER BY ur.merchant_id
	`)
	if err != nil {
		return nil, fmt.Errorf("count eligible rows: %w", err)
	}
	defer rows.Close()

	var counts []merchantCount
	for rows.Next() {
		var c merchantCount
		if err := rows.Scan(&c.merchantID, &c.count); err != nil {
			return nil, fmt.Errorf("scan eligible count: %w", err)
		}
		counts = append(counts, c)
	}
	return counts, rows.Err()
}

// missingAdminMerchants returns the distinct merchant_id values that have at
// least one users_rights row with role_id IS NULL but no "admin" role at all
// — these are left untouched by runApply and must be reported, not silently
// skipped.
func missingAdminMerchants(ctx context.Context, q dbutils.DBTX) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT DISTINCT ur.merchant_id
		FROM users_rights ur
		WHERE ur.role_id IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM roles r WHERE r.merchant_id = ur.merchant_id AND r.system_key = 'admin'
		  )
		ORDER BY ur.merchant_id
	`)
	if err != nil {
		return nil, fmt.Errorf("find merchants without admin role: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, fmt.Errorf("scan merchant_id: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func runDryRun(ctx context.Context, db *sql.DB) {
	q := dbx.GetDB(ctx, db)

	counts, err := eligibleCounts(ctx, q)
	if err != nil {
		log.Fatalf("dry-run: %v", err)
	}
	missing, err := missingAdminMerchants(ctx, q)
	if err != nil {
		log.Fatalf("dry-run: %v", err)
	}

	printReport("DRY-RUN — nothing written", counts, missing)
}

func runApply(ctx context.Context, db *sql.DB) {
	var counts []merchantCount
	var missing []string

	err := dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
		q := dbx.GetDB(txCtx, db)

		eligible, err := eligibleCounts(txCtx, q)
		if err != nil {
			return err
		}
		missing, err = missingAdminMerchants(txCtx, q)
		if err != nil {
			return err
		}

		for _, c := range eligible {
			res, err := q.ExecContext(txCtx, `
				UPDATE users_rights
				SET role_id = (
					SELECT r.id FROM roles r WHERE r.merchant_id = ? AND r.system_key = 'admin'
				)
				WHERE merchant_id = ? AND role_id IS NULL
			`, c.merchantID, c.merchantID)
			if err != nil {
				return fmt.Errorf("assign admin role for merchant %s: %w", c.merchantID, err)
			}
			affected, err := res.RowsAffected()
			if err != nil {
				return fmt.Errorf("rows affected for merchant %s: %w", c.merchantID, err)
			}
			counts = append(counts, merchantCount{merchantID: c.merchantID, count: affected})
		}
		return nil
	})
	if err != nil {
		log.Fatalf("apply: %v", err)
	}

	printReport("APPLY — committed", counts, missing)
}

func printReport(header string, counts []merchantCount, missing []string) {
	fmt.Println(header)
	var total int64
	for _, c := range counts {
		fmt.Printf("  merchant %s: %d row(s)\n", c.merchantID, c.count)
		total += c.count
	}
	fmt.Printf("TOTAL: %d row(s) across %d merchant(s)\n", total, len(counts))

	if len(missing) == 0 {
		return
	}
	fmt.Println("Merchants with NO admin role — left untouched, run cmd/seed_system_roles first:")
	for _, m := range missing {
		fmt.Printf("  - %s\n", m)
	}
}
