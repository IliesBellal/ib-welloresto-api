//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"database/sql"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/modules/customers"
)

// LOT A Semaine 1, Chantier 2 (docs/decisions.md) : DenyOrderLocal était le
// seul des trois points de clôture de commande (SetDeliveredLocal,
// DeleteOrderLocal, DenyOrderLocal) à ne jamais écrire hash/previous_hash/
// signature — une commande refusée par le marchand sortait de la chaîne
// fiscale sans laisser de trace. Vérifie ici que ce n'est plus le cas, et
// que le chaînage (previous_hash -> hash de la commande précédente) est
// effectif entre deux refus successifs du même marchand.
func TestOrderLifeCycleRepository_DenyOrderLocal_FiscalChain_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-olc-deny-fc' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest OLC Deny FC', 'a', '1', 's', '75001', 'Paris', 'siret-olc-deny-fc', 'https://x', '06', 'mtok-olc-deny-fc', 'UTC')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)

	custoRepo := customers.NewCustomerRepository(db)
	repo := NewOrdersLifeCycleRepository(db, custoRepo)

	newOpenOrder := func(t *testing.T, orderNum int) string {
		t.Helper()
		var orderID int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, price, TVA, HT, created_by)
			VALUES ($1, $2, 'WELLO_RESTO', 'PENDING', 'OPEN', 1000, 0, 1000, 'itest')
			RETURNING order_id`, merchantID, orderNum).Scan(&orderID); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		return strconv.FormatInt(orderID, 10)
	}

	readFiscalColumns := func(t *testing.T, orderID string) (previousHash, hash, signature sql.NullString) {
		t.Helper()
		if err := db.QueryRowContext(ctx, `SELECT previous_hash, hash, signature FROM orders WHERE order_id = $1`, orderID).
			Scan(&previousHash, &hash, &signature); err != nil {
			t.Fatalf("read back fiscal columns: %v", err)
		}
		return
	}

	// Premier refus pour ce marchand : aucune commande CLOSED précédente ->
	// GENESIS_HASH, comme cash_registers pour son propre premier maillon.
	orderA := newOpenOrder(t, 3000)
	if err := repo.DenyOrderLocal(ctx, orderA, "1", "itest deny fiscal chain A", "226"); err != nil {
		t.Fatalf("DenyOrderLocal (A): %v", err)
	}
	prevA, hashA, sigA := readFiscalColumns(t, orderA)
	if !hashA.Valid || hashA.String == "" {
		t.Fatalf("DenyOrderLocal (A): expected non-null/non-empty hash, got %v", hashA)
	}
	if !sigA.Valid || sigA.String == "" {
		t.Fatalf("DenyOrderLocal (A): expected non-null/non-empty signature, got %v", sigA)
	}
	if !prevA.Valid || prevA.String != "GENESIS_HASH" {
		t.Fatalf("DenyOrderLocal (A): expected previous_hash=GENESIS_HASH, got %v", prevA)
	}

	// Deuxième refus pour le même marchand : previous_hash doit chaîner sur
	// le hash de la commande A.
	orderB := newOpenOrder(t, 3001)
	if err := repo.DenyOrderLocal(ctx, orderB, "1", "itest deny fiscal chain B", "226"); err != nil {
		t.Fatalf("DenyOrderLocal (B): %v", err)
	}
	prevB, hashB, sigB := readFiscalColumns(t, orderB)
	if !hashB.Valid || hashB.String == "" {
		t.Fatalf("DenyOrderLocal (B): expected non-null/non-empty hash, got %v", hashB)
	}
	if !sigB.Valid || sigB.String == "" {
		t.Fatalf("DenyOrderLocal (B): expected non-null/non-empty signature, got %v", sigB)
	}
	if !prevB.Valid || prevB.String != hashA.String {
		t.Fatalf("DenyOrderLocal (B): expected previous_hash=%q (hash of A), got %v", hashA.String, prevB)
	}
	if hashB.String == hashA.String {
		t.Fatalf("DenyOrderLocal (A) and (B): expected distinct hashes, both are %q", hashA.String)
	}
}
