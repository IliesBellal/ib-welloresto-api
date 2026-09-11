//go:build postgres_integration

package signup_test

import (
	"context"
	"fmt"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// TestUqMerchantSiretValid_Postgres proves migration 132's partial unique
// index actually enforces what LOT A Semaine 3 Chantier 10 asked for: two
// ACTIVE merchants can never share a valid-format SIRET (the concurrent race
// isSIRETUniqueViolation in service.go exists to translate), while an
// inactive merchant's SIRET can be reused by a new active one, and malformed
// SIRETs (outside the historical-parc carve-out) are never constrained.
func TestUqMerchantSiretValid_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	tokenSeq := 0
	insertMerchant := func(siret string, isActive bool) (int64, error) {
		tokenSeq++
		var id int64
		err := db.QueryRowContext(ctx, `
			INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, is_active)
			VALUES ('ITest SIRET Unique', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', $2, 'UTC', $3)
			RETURNING id`, siret, fmt.Sprintf("itest-su-%d", tokenSeq), isActive).Scan(&id)
		return id, err
	}

	const validSiret = "73282932000074" // real, Luhn-valid (INSEE/La Poste's own — same one handler_postgres_integration_test.go uses)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE siret = $1`, validSiret)
	})

	// --- first active merchant with this SIRET: succeeds ---
	id1, err := insertMerchant(validSiret, true)
	if err != nil {
		t.Fatalf("first active insert: %v", err)
	}

	// --- second active merchant, same SIRET: rejected by the index ---
	if _, err := insertMerchant(validSiret, true); err == nil {
		t.Fatal("second active merchant with the same valid SIRET: expected a unique violation, got nil")
	}

	// --- deactivate the first, then a new active merchant CAN reuse the SIRET ---
	if _, err := db.ExecContext(ctx, `UPDATE merchant SET is_active = false WHERE id = $1`, id1); err != nil {
		t.Fatalf("deactivate first merchant: %v", err)
	}
	if _, err := insertMerchant(validSiret, true); err != nil {
		t.Fatalf("reuse SIRET after deactivating the previous holder: expected success, got: %v", err)
	}

	// --- reactivating the first (still holding the same SIRET) now collides again ---
	if _, err := db.ExecContext(ctx, `UPDATE merchant SET is_active = true WHERE id = $1`, id1); err == nil {
		t.Fatal("reactivating the original merchant while a second active one holds the same SIRET: expected a unique violation, got nil")
	}
}
