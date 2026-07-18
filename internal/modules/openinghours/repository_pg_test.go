package openinghours

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"welloresto-api/internal/database/dbx"
)

// TestFetchActiveSlots_Postgres est un test d'intégration contre le Postgres
// Docker de dev (docker-compose.postgres.yml). Ignoré sans POSTGRES_TEST_URL :
//
//	POSTGRES_TEST_URL="postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev" \
//	  go test ./internal/modules/openinghours/... -run Postgres
func TestFetchActiveSlots_Postgres(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_URL")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_URL non défini — test d'intégration Postgres ignoré")
	}
	t.Setenv(dbx.EnvDialect, "postgres")

	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := database.PingContext(ctx); err != nil {
		t.Fatalf("postgres injoignable: %v", err)
	}

	// Même DDL que docs/migration-postgres/04-schema-postgres-target.sql.
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS hours_of_operation (
			id varchar(64) NOT NULL,
			merchant_id varchar(64) NOT NULL,
			day_of_week_from integer NOT NULL,
			hour_from time NOT NULL,
			day_of_week_to integer NOT NULL,
			hour_to time NOT NULL,
			first_booking_time time,
			last_booking_time time,
			booking_capacity integer NOT NULL DEFAULT 0,
			valid_from timestamptz,
			valid_to timestamptz,
			creation_date timestamptz NOT NULL DEFAULT now(),
			enabled boolean NOT NULL DEFAULT true,
			PRIMARY KEY (id)
		)`); err != nil {
		t.Fatal(err)
	}

	merchantID := "test-openinghours-pg"
	cleanup := func() {
		_, _ = database.ExecContext(ctx, `DELETE FROM hours_of_operation WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	db := dbx.GetDB(ctx, database)
	insert := `
		INSERT INTO hours_of_operation
			(id, merchant_id, day_of_week_from, hour_from, day_of_week_to, hour_to, valid_from, valid_to, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	rows := []struct {
		id       string
		dayFrom  int
		hourFrom string
		dayTo    int
		hourTo   string
		validTo  interface{}
		enabled  bool
	}{
		{"t-hoo-1", 1, "09:00:00", 1, "14:00:00", nil, true},  // lundi midi
		{"t-hoo-2", 3, "09:00:00", 3, "14:00:00", nil, true},  // mercredi midi
		{"t-hoo-3", 3, "18:00:00", 3, "23:00:00", nil, true},  // mercredi soir
		{"t-hoo-4", 3, "00:00:00", 3, "23:59:59", nil, false}, // désactivé
		{"t-hoo-5", 3, "00:00:00", 3, "23:59:59", "2020-01-01 00:00:00", true}, // fenêtre expirée
	}
	for _, row := range rows {
		if _, err := db.ExecContext(ctx, insert,
			row.id, merchantID, row.dayFrom, row.hourFrom, row.dayTo, row.hourTo,
			nil, row.validTo, row.enabled,
		); err != nil {
			t.Fatalf("insert %s: %v", row.id, err)
		}
	}

	// Mercredi 2026-07-15 12:00 heure locale marchand.
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

	slots, err := FetchActiveSlots(ctx, database, merchantID, now)
	if err != nil {
		t.Fatalf("FetchActiveSlots: %v", err)
	}
	if len(slots) != 3 {
		t.Fatalf("expected 3 active slots (disabled + expired excluded), got %d: %+v", len(slots), slots)
	}

	status := ComputePOSStatus(now, slots)
	if !status.IsOpen {
		t.Fatal("IsOpen = false, want true (mercredi midi)")
	}
	assertBound(t, "CurrentStart", status.CurrentStart, "2026-07-15 09:00:00")
	assertBound(t, "CurrentEnd", status.CurrentEnd, "2026-07-15 14:00:00")
	assertBound(t, "LastStart", status.LastStart, "2026-07-13 09:00:00")
	assertBound(t, "NextStart", status.NextStart, "2026-07-15 18:00:00")
	assertBound(t, "NextEnd", status.NextEnd, "2026-07-15 23:00:00")
}
