package database

import (
	"database/sql"
	"time"
	"welloresto-api/internal/config"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func NewPostgres(dsn config.DatabaseConfig) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn.PostgresURL)
	if err != nil {
		return nil, err
	}

	// Mêmes options de pool que la config MySQL le temps de la migration
	db.SetMaxOpenConns(15)                 // Maximum 1 connexion ouverte en même temps
	db.SetMaxIdleConns(4)                  // Maximum 1 connexion en attente
	db.SetConnMaxLifetime(5 * time.Minute) // Renouveler la connexion régulièrement
	db.SetConnMaxIdleTime(1 * time.Minute) // Aligné sur la config MySQL

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}

// AnalyticsMaxOpenConns caps the low-priority analytics pool. Kept far below
// NewPostgres's 15 so analytics can never starve the POS pool of
// connections — it fails to acquire one instead (fast, visible error) rather
// than queueing behind order-taking traffic.
//
// Budget math (see docs/analytics/MESURES.md, wello-back-office repo,
// "Fusible" section, for the full derivation): the Render Basic instance has
// 256MB RAM, 64MB of which is shared_buffers. Worst case is
// work_mem × sort/hash nodes per query × concurrent analytics connections.
//
// PROMPT 24 Phase 5 re-derives this budget for the multi-establishment
// selector (up to a few concurrent comparing users, no code-enforced cap on
// establishment count — see ValidateRequestedMerchants's doc comment): raised
// from 2 to 4 connections, AnalyticsWorkMemMB dropped from 16 to 8 in the same
// commit so the worst-case contribution stays IDENTICAL — 4×4×8 = 128MB,
// same as the old 2×4×16 = 128MB — +64MB shared_buffers = 192MB, the same
// 192MB the instance has run on since this fusible was first sized. This is
// twice the concurrent analytics connections for the same memory ceiling, not
// a memory increase. Raising AnalyticsMaxOpenConns again requires re-deriving
// this budget, not just bumping the constant — and per-query work_mem cannot
// be dropped further without re-measuring for disk spill first (see
// AnalyticsWorkMemMB's doc comment).
const AnalyticsMaxOpenConns = 4

// AnalyticsStatementTimeoutMS is applied as `SET LOCAL statement_timeout` on
// every analytics transaction (internal/modules/analytics), never globally —
// a POS-critical connection on the same instance must never inherit it. A
// query that runs longer fails fast with a readable Postgres error instead of
// holding a connection (and CPU, on a 0.1 vCPU instance shared with order
// taking) indefinitely.
const AnalyticsStatementTimeoutMS = 4000

// AnalyticsWorkMemMB is applied as `SET LOCAL work_mem` on every analytics
// transaction. PERIMETRE.md §2.3 measured a ×1.24 gain and the elimination of
// disk spill at 64MB on an isolated query — that number is a test-bench
// result, not a production budget: see AnalyticsMaxOpenConns's comment for
// the budget math that actually sizes this constant.
//
// PROMPT 24 Phase 5 dropped this from 16 to 8 (see AnalyticsMaxOpenConns's
// doc comment for why, in the same commit that doubled the connection count).
// Re-measured against staging on Options/Produits — the two heaviest queries
// in this package (widest GROUP BY, largest joins) — via
// EXPLAIN (ANALYZE, BUFFERS) with work_mem set to 8MB in the same session:
// no "temp read"/"temp written" line on either plan at realistic staging data
// volumes (see docs/decisions.md, this repo, PROMPT 24 Phase 5, for the exact
// queries and numbers). If a future query DOES spill at 8MB, that is a real
// arbitrage between connection count and per-query memory — not a threshold
// to quietly raise back without re-deriving AnalyticsMaxOpenConns's budget.
const AnalyticsWorkMemMB = 8

// NewAnalyticsPostgres opens the dedicated low-priority pool used only by
// internal/modules/analytics. Reads ANALYTICS_DATABASE_URL (falling back to
// POSTGRES_URL — see config.loadDatabase) so pointing analytics at a replica
// later is an env var change, not a code change.
func NewAnalyticsPostgres(dsn config.DatabaseConfig) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn.AnalyticsPostgresURL)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(AnalyticsMaxOpenConns)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(1 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}
