//go:build postgres_integration

package ubereats

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Conformité caisse, C10 (lots A et B) : les clôtures Uber Eats par
// réconciliation. Vente entièrement payée : clôture datée + ticket ; vente pas
// entièrement payée : reste ouverte ; annulation : clôture datée sans ticket ;
// commande déjà close : statut seulement, date de clôture intacte. Les
// commandes sont scellées par leur clôture journalière (fiscal.CloseDueDays).
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
		state, brandStatus string
		hash, cancelledBy  sql.NullString
		deliveredOn        sql.NullTime
	}
	read := func(t *testing.T, orderID string) row {
		t.Helper()
		var r row
		if err := db.QueryRowContext(ctx, `
			SELECT state, brand_status, hash, cancelled_by_type, delivered_on
			FROM orders WHERE order_id = $1`, orderID).
			Scan(&r.state, &r.brandStatus, &r.hash, &r.cancelledBy, &r.deliveredOn); err != nil {
			t.Fatalf("read order: %v", err)
		}
		return r
	}
	var issued []string
	issuer := func(_ context.Context, m, o string) error {
		issued = append(issued, m+"/"+o)
		return nil
	}

	// Vente entièrement payée : clôturée et datée (sa clôture journalière la
	// scellera), ticket émis ; plus d'empreinte sur la ligne (lot B).
	sale := seed(t, "itest-c10-sale", true)
	if err := repo.SyncOrderState(ctx, "itest-c10-sale", StatusCompleted, StateClosed, "ACCEPTED", sql.NullInt64{}, issuer); err != nil {
		t.Fatalf("SyncOrderState (sale): %v", err)
	}
	saleRow := read(t, sale)
	if saleRow.state != "CLOSED" || saleRow.brandStatus != StatusCompleted || !saleRow.deliveredOn.Valid || saleRow.hash.Valid {
		t.Fatalf("sale: expected CLOSED/COMPLETED dated, no row seal, got %+v", saleRow)
	}
	if len(issued) != 1 || issued[0] != merchantID+"/"+sale {
		t.Fatalf("sale: expected one receipt for %s/%s, got %v", merchantID, sale, issued)
	}

	// Vente pas entièrement payée : reste ouverte, rien n'est émis.
	unpaid := seed(t, "itest-c10-unpaid", false)
	if err := repo.SyncOrderState(ctx, "itest-c10-unpaid", StatusCompleted, StateClosed, "ACCEPTED", sql.NullInt64{}, issuer); err != nil {
		t.Fatalf("SyncOrderState (unpaid): %v", err)
	}
	if r := read(t, unpaid); r.state != "OPEN" || r.deliveredOn.Valid || len(issued) != 1 {
		t.Fatalf("unpaid sale: expected left OPEN, undated, no receipt; got %+v, receipts %v", r, issued)
	}

	// Vente sans émetteur de ticket branché : échec, rien n'est écrit.
	noIssuer := seed(t, "itest-c10-noissuer", true)
	if err := repo.SyncOrderState(ctx, "itest-c10-noissuer", StatusCompleted, StateClosed, "ACCEPTED", sql.NullInt64{}, nil); err == nil {
		t.Fatal("sale without receipt issuer: expected an error")
	}
	if r := read(t, noIssuer); r.state != "OPEN" || r.deliveredOn.Valid {
		t.Fatalf("sale without receipt issuer: expected rolled back (OPEN, undated), got %+v", r)
	}

	// Annulation : clôturée et datée, sans ticket.
	canceled := seed(t, "itest-c10-cancel", true)
	if err := repo.SyncOrderState(ctx, "itest-c10-cancel", StatusCanceled, StateClosed, "ACCEPTED", sql.NullInt64{Int64: 39, Valid: true}, issuer); err != nil {
		t.Fatalf("SyncOrderState (cancel): %v", err)
	}
	cancelRow := read(t, canceled)
	if cancelRow.state != "CLOSED" || cancelRow.brandStatus != StatusCanceled || cancelRow.cancelledBy.String != "PLATFORM" ||
		!cancelRow.deliveredOn.Valid || cancelRow.hash.Valid {
		t.Fatalf("cancel: expected CLOSED/CANCELED/PLATFORM dated, got %+v", cancelRow)
	}
	if len(issued) != 1 {
		t.Fatalf("cancel: no receipt expected, got %v", issued)
	}

	// Commande déjà close (la vente) : statut seulement, date de clôture
	// inchangée (elle rattache la commande à sa clôture journalière).
	time.Sleep(10 * time.Millisecond)
	if err := repo.HandleOrderNotFound(ctx, "itest-c10-sale", issuer); err != nil {
		t.Fatalf("HandleOrderNotFound (closed): %v", err)
	}
	after := read(t, sale)
	if after.brandStatus != "CANCELED" || after.deliveredOn != saleRow.deliveredOn || len(issued) != 1 {
		t.Fatalf("closed order: expected status-only update (CANCELED), closing date unchanged, no receipt; got %+v", after)
	}

	// Uber repasse la commande close à ACCEPTED : statut seulement, elle
	// n'est jamais rouverte (lot C conformité caisse, R1 bis).
	if err := repo.SyncOrderState(ctx, "itest-c10-sale", StatusAccepted, StateOpen, "ACCEPTED", sql.NullInt64{}, issuer); err != nil {
		t.Fatalf("SyncOrderState (accepted on closed): %v", err)
	}
	if reopened := read(t, sale); reopened.state != "CLOSED" || reopened.brandStatus != StatusAccepted || reopened.deliveredOn != saleRow.deliveredOn {
		t.Fatalf("closed order reopened by the platform: %+v", reopened)
	}
}
