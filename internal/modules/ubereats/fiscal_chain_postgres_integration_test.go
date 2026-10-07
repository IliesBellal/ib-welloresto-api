//go:build postgres_integration

package ubereats

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/fiscal"
)

// Lot A conformité caisse, C10 (docs/attestation-conformite-01-lot-A-brief.md,
// phase 4) : les clôtures Uber Eats par réconciliation passent par la chaîne
// fiscale. Vente entièrement payée : clôture scellée + ticket ; vente pas
// entièrement payée : reste ouverte ; annulation : clôture scellée sans
// ticket ; commande déjà close : statut seulement, scellement intact.
func TestUberReconciliation_FiscalChain_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const siret = "siret-ue-c10"
	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id::text = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = $1 LIMIT 1`, siret).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	var mid int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest UE C10', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mtok-ue-c10', 'UTC')
		RETURNING id`, siret).Scan(&mid); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(mid, 10)
	t.Cleanup(func() { cleanupFor(merchantID) })

	repo := NewUberEatsRepository(db)
	num := 9000
	seed := func(t *testing.T, brandOrderID string, paid bool) string {
		t.Helper()
		num++
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_order_id, brand_status, state, order_type, price, TVA, HT, created_by)
			VALUES ($1, $2, 'UBER_EATS', $3, 'ACCEPTED', 'OPEN', 'DELIVERY', 2000, 182, 1818, 'UBER_EATS')
			RETURNING order_id`, merchantID, num, brandOrderID).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		if paid {
			if _, err := db.ExecContext(ctx, `
				INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, enabled)
				VALUES ($1, 'UBER_EATS', $2, 2000, 'UBER_EATS', true)`, merchantID, id); err != nil {
				t.Fatalf("seed payment: %v", err)
			}
		}
		return strconv.FormatInt(id, 10)
	}
	type row struct {
		state, brandStatus      string
		prev, hash, cancelledBy sql.NullString
		deliveredOn             sql.NullTime
		version                 int
	}
	read := func(t *testing.T, orderID string) row {
		t.Helper()
		var r row
		if err := db.QueryRowContext(ctx, `
			SELECT state, brand_status, previous_hash, hash, cancelled_by_type, delivered_on, hash_version
			FROM orders WHERE order_id = $1`, orderID).
			Scan(&r.state, &r.brandStatus, &r.prev, &r.hash, &r.cancelledBy, &r.deliveredOn, &r.version); err != nil {
			t.Fatalf("read order: %v", err)
		}
		return r
	}
	var issued []string
	issuer := func(_ context.Context, m, o string) error {
		issued = append(issued, m+"/"+o)
		return nil
	}

	// Vente entièrement payée : clôture scellée, reproductible, et ticket.
	sale := seed(t, "itest-c10-sale", true)
	if err := repo.SyncOrderState(ctx, "itest-c10-sale", StatusCompleted, StateClosed, "ACCEPTED", sql.NullInt64{}, issuer); err != nil {
		t.Fatalf("SyncOrderState (sale): %v", err)
	}
	saleRow := read(t, sale)
	if saleRow.state != "CLOSED" || saleRow.brandStatus != StatusCompleted || saleRow.version != fiscal.HashVersion || !saleRow.hash.Valid {
		t.Fatalf("sale: expected CLOSED/COMPLETED sealed v2, got %+v", saleRow)
	}
	if saleRow.prev.String != fiscal.GenesisHash {
		t.Fatalf("sale: first link of the merchant chain, expected GENESIS_HASH, got %q", saleRow.prev.String)
	}
	payload, err := fiscal.LoadOrderClosure(ctx, dbx.GetDB(ctx, db), sale, saleRow.deliveredOn.Time)
	if err != nil {
		t.Fatalf("LoadOrderClosure: %v", err)
	}
	if h, _, _ := fiscal.Seal(fiscal.ChainOrders, saleRow.prev.String, payload); h != saleRow.hash.String {
		t.Fatal("sale: closure seal not reproducible from the database")
	}
	if len(issued) != 1 || issued[0] != merchantID+"/"+sale {
		t.Fatalf("sale: expected one receipt for %s/%s, got %v", merchantID, sale, issued)
	}

	// Vente pas entièrement payée : reste ouverte, rien n'est scellé ni émis.
	unpaid := seed(t, "itest-c10-unpaid", false)
	if err := repo.SyncOrderState(ctx, "itest-c10-unpaid", StatusCompleted, StateClosed, "ACCEPTED", sql.NullInt64{}, issuer); err != nil {
		t.Fatalf("SyncOrderState (unpaid): %v", err)
	}
	if r := read(t, unpaid); r.state != "OPEN" || r.hash.Valid || len(issued) != 1 {
		t.Fatalf("unpaid sale: expected left OPEN, unsealed, no receipt; got %+v, receipts %v", r, issued)
	}

	// Vente sans émetteur de ticket branché : échec, rien n'est écrit.
	noIssuer := seed(t, "itest-c10-noissuer", true)
	if err := repo.SyncOrderState(ctx, "itest-c10-noissuer", StatusCompleted, StateClosed, "ACCEPTED", sql.NullInt64{}, nil); err == nil {
		t.Fatal("sale without receipt issuer: expected an error")
	}
	if r := read(t, noIssuer); r.state != "OPEN" || r.hash.Valid {
		t.Fatalf("sale without receipt issuer: expected rolled back (OPEN, unsealed), got %+v", r)
	}

	// Annulation : clôture scellée, chaînée sur la vente, sans ticket.
	canceled := seed(t, "itest-c10-cancel", true)
	if err := repo.SyncOrderState(ctx, "itest-c10-cancel", StatusCanceled, StateClosed, "ACCEPTED", sql.NullInt64{Int64: 39, Valid: true}, issuer); err != nil {
		t.Fatalf("SyncOrderState (cancel): %v", err)
	}
	cancelRow := read(t, canceled)
	if cancelRow.state != "CLOSED" || cancelRow.brandStatus != StatusCanceled || cancelRow.cancelledBy.String != "PLATFORM" ||
		!cancelRow.deliveredOn.Valid || cancelRow.version != fiscal.HashVersion || cancelRow.prev.String != saleRow.hash.String {
		t.Fatalf("cancel: expected CLOSED/CANCELED/PLATFORM sealed after the sale, got %+v", cancelRow)
	}
	if len(issued) != 1 {
		t.Fatalf("cancel: no receipt expected, got %v", issued)
	}

	// Commande déjà close (la vente) : statut seulement, scellement intact.
	time.Sleep(10 * time.Millisecond)
	if err := repo.HandleOrderNotFound(ctx, "itest-c10-sale", issuer); err != nil {
		t.Fatalf("HandleOrderNotFound (closed): %v", err)
	}
	after := read(t, sale)
	if after.brandStatus != "CANCELED" || after.hash != saleRow.hash || after.deliveredOn != saleRow.deliveredOn || len(issued) != 1 {
		t.Fatalf("closed order: expected status-only update (CANCELED), seal and closing date unchanged, no receipt; got %+v", after)
	}
}
