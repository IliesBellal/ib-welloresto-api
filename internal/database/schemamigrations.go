package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
)

// WarnUnrecordedMigrations compares migrations/todo/*.up.sql against the
// schema_migrations table (PROMPT 27 Phase 2 —
// docs/migration-postgres/67-migration-status-audit.md §4) and logs a
// warning listing any file with no matching row.
//
// Deliberately never Fatal, and never blocks startup: this project applies
// migrations by hand (CLAUDE.md, "no migration tool"), so a file prepared
// but not yet played — 112_pg_stat_statements, 113_drop_users_rights_admin_column,
// 117_cleanup_deletion_reason_id_quotes, 120_drop_cart_discount_legacy_columns
// today — is a legitimate, long-lived state, not an incident. The point is
// visibility (a line in the Render deploy logs someone can notice), not
// enforcement.
//
// Matches by exact filename, not by leading number: 103_permission_catalog_lot10.up.sql
// and 103_production_ready_delivery_arrival.up.sql share the number "103"
// (migrations/migrations_numbering_test.go documents the collision as
// legitimate) but schema_migrations disambiguates them as "103a"/"103b" —
// comparing by number alone would conflate the two. filename is not a
// declared UNIQUE constraint on schema_migrations (version is the PK), but
// in practice every row this project writes carries a distinct filename.
//
// migrationsDir is a relative or absolute path to migrations/todo. If it
// can't be read (e.g. the working directory isn't the repo root, or this
// runs somewhere migrations/ wasn't deployed), this logs a warning and
// returns — never fails startup over a missing directory either.
func WarnUnrecordedMigrations(ctx context.Context, db *sql.DB, logger *zap.Logger, migrationsDir string) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		logger.Warn("schema_migrations check: could not read migrations directory, skipping",
			zap.String("dir", migrationsDir), zap.Error(err))
		return
	}

	var upFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".up.sql") {
			upFiles = append(upFiles, name)
		}
	}
	if len(upFiles) == 0 {
		return
	}

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var tableExists bool
	err = db.QueryRowContext(checkCtx, `SELECT to_regclass('public.schema_migrations') IS NOT NULL`).Scan(&tableExists)
	if err != nil {
		logger.Warn("schema_migrations check: could not query the database, skipping", zap.Error(err))
		return
	}
	if !tableExists {
		// Legitimate on a fresh/local environment before migration 123 has
		// been played, or on MySQL. Not itself worth a warning — the table's
		// absence isn't a drift signal, just "not set up yet here".
		return
	}

	rows, err := db.QueryContext(checkCtx, `SELECT filename FROM schema_migrations`)
	if err != nil {
		logger.Warn("schema_migrations check: could not read recorded migrations, skipping", zap.Error(err))
		return
	}
	defer rows.Close()

	recorded := make(map[string]bool, len(upFiles))
	for rows.Next() {
		var filename string
		if err := rows.Scan(&filename); err != nil {
			logger.Warn("schema_migrations check: error scanning row, skipping", zap.Error(err))
			return
		}
		recorded[filename] = true
	}
	if err := rows.Err(); err != nil {
		logger.Warn("schema_migrations check: error reading rows, skipping", zap.Error(err))
		return
	}

	var unrecorded []string
	for _, f := range upFiles {
		if !recorded[f] {
			unrecorded = append(unrecorded, f)
		}
	}
	if len(unrecorded) == 0 {
		return
	}
	sort.Strings(unrecorded)

	logger.Warn("schema_migrations: fichiers de migrations/todo sans ligne correspondante — appliquée manuellement sans l'enregistrer, ou préparée-non-jouée par choix (ex. 112/113/117/120, voir CLAUDE.md)",
		zap.Strings("files", unrecorded),
		zap.String("dir", filepath.Clean(migrationsDir)))
}
