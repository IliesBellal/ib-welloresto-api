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
// With AnalyticsWorkMemMB=16 and a conservative 4 memory-consuming nodes per
// query (GROUP BY + ORDER BY + a join hash + a window/aggregate), 2 analytics
// connections cap contribution at 16×4×2 = 128MB, +64MB shared_buffers =
// 192MB, leaving 64MB for OS + per-backend overhead + the POS pool's own
// (much smaller, default work_mem) usage. Raising this above 2 requires
// re-deriving the budget, not just bumping the constant.
const AnalyticsMaxOpenConns = 2

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
// why 16MB is the value actually deployed here.
const AnalyticsWorkMemMB = 16

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
