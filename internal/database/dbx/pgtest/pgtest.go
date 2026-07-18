// Package pgtest fournit l'ouverture de connexion pour les tests
// d'intégration Postgres (fichiers tagués `postgres_integration`).
// Ces tests s'exécutent contre le Postgres Docker de dev :
//
//	DB_DIALECT=postgres POSTGRES_URL=postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev \
//	  go test -tags postgres_integration ./internal/modules/<module>/...
package pgtest

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Open retourne une connexion au Postgres de test, ou skip si POSTGRES_URL
// n'est pas défini. Force DB_DIALECT=postgres pour la durée du test.
func Open(t *testing.T) *sql.DB {
	t.Helper()

	url := os.Getenv("POSTGRES_URL")
	if url == "" {
		t.Skip("POSTGRES_URL not set — skipping postgres integration test")
	}
	t.Setenv("DB_DIALECT", "postgres")

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
