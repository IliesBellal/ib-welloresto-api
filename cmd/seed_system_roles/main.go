// Command seed_system_roles is the one-shot data population step for
// migrations/done/096_seed_system_roles.up.sql.
//
// Why a Go program and not plain SQL: role ids must be
// "role-<uuid>" generated with helpers.GeneratePrefixedID(helpers.RoleIDPrefix)
// (uuid.New(), a Go-side random v4 UUID) — the same convention every other
// application-generated primary key in this codebase follows. Postgres has
// gen_random_uuid()/uuid_generate_v4() but MySQL has no equivalent extension
// enabled in this project, and the repo is mid-migration between the two
// dialects (DB_DIALECT). A SQL-only seed would need two divergent
// implementations for a one-time operation. Going through
// roles.Repository.EnsureSystemRoles instead means this script is the exact
// same code path as everything else that creates system roles (e.g.
// POSService.CreateMerchant for new merchants) — there is exactly one
// implementation of "what a system role looks like", not a second one to
// drift out of sync.
//
// For every existing merchant, this:
//  1. ensures the "admin" and "staff" system roles exist (idempotent — a
//     merchant that already has them keeps the same role ids and name);
//  2. reconciles each system role's permissions against
//     roles.systemRolePermissions, granting whatever is missing (this is how
//     RBAC lot 2's pos.status.manage backfills onto every admin role created
//     back in lot 1 — see roles.Repository.ensureSystemRole). Never revokes a
//     permission the role already has.
//  3. points merchant.default_role_id at the "admin" role, but only if
//     default_role_id is still NULL (never overwrites a merchant that was
//     already repointed to something else). RBAC lot 4 decision: every
//     account becomes Administrateur while permissions are not yet exploited
//     from the UI, so a newly provisioned merchant's default must match —
//     see migrations/done/099_merchant_default_role_admin.up.sql for the
//     one-time correction applied to merchants seeded before this decision.
//
// Re-running this script after the permission catalog grows again (a future
// migration 09X) is the supported way to backfill existing merchants —
// nothing else needs to change here.
//
// It does NOT set users_rights.role_id anywhere — no user is attached to a
// role by this lot, on purpose (see migrations/done/096_seed_system_roles.up.sql).
//
// Usage:
//
//	DB_DIALECT=postgres POSTGRES_URL=postgres://... go run ./cmd/seed_system_roles
package main

import (
	"context"
	"database/sql"
	"log"
	"strconv"

	"welloresto-api/internal/config"
	"welloresto-api/internal/database"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/modules/roles"
)

// merchantRef carries both representations of a merchant id this script
// needs: the integer form to match merchant.id (an integer PK), and the
// string form used everywhere else (roles.merchant_id, users_rights.merchant_id, ...).
type merchantRef struct {
	intID int64
	strID string
}

func main() {
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
	rolesRepo := roles.NewRepository(db)

	merchants, err := listMerchants(ctx, db)
	if err != nil {
		log.Fatalf("list merchants: %v", err)
	}

	for _, merchant := range merchants {
		adminRoleID, _, err := rolesRepo.EnsureSystemRoles(ctx, merchant.strID)
		if err != nil {
			log.Fatalf("ensure system roles for merchant %s: %v", merchant.strID, err)
		}

		if err := setDefaultRoleIfUnset(ctx, db, merchant.intID, adminRoleID); err != nil {
			log.Fatalf("set default_role_id for merchant %s: %v", merchant.strID, err)
		}

		log.Printf("merchant %s: system roles ensured, default_role_id -> admin (%s)", merchant.strID, adminRoleID)
	}

	log.Printf("done: %d merchant(s) processed", len(merchants))
}

func listMerchants(ctx context.Context, db *sql.DB) ([]merchantRef, error) {
	d := dbx.GetDB(ctx, db)
	rows, err := d.QueryContext(ctx, `SELECT id FROM merchant ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var merchants []merchantRef
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		merchants = append(merchants, merchantRef{intID: id, strID: strconv.FormatInt(id, 10)})
	}
	return merchants, rows.Err()
}

func setDefaultRoleIfUnset(ctx context.Context, db *sql.DB, merchantIntID int64, staffRoleID string) error {
	d := dbx.GetDB(ctx, db)
	_, err := d.ExecContext(ctx, `
		UPDATE merchant SET default_role_id = ? WHERE id = ? AND default_role_id IS NULL
	`, staffRoleID, merchantIntID)
	return err
}
