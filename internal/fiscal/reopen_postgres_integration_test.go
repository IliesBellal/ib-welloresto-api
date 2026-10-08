//go:build postgres_integration

package fiscal

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
)

// Réouverture d'une commande close (lot C conformité caisse, R1 :
// docs/attestation-conformite-03-lot-C-brief.md).
func TestReopenOrder_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const siret = "siret-itest-reopen"

	cleanup := func(mid string) {
		for _, q := range []string{
			`DELETE FROM fiscal_closures WHERE merchant_id = $1`,
			`DELETE FROM receipts WHERE merchant_id = $1`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM cash_registers WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id::text = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var old int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = $1 LIMIT 1`, siret).Scan(&old); err == nil {
		cleanup(strconv.FormatInt(old, 10))
	}
	var mid int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, creation_date)
		VALUES ('ITest Reopen', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mt-reopen', 'Europe/Paris', '2025-01-01')
		RETURNING id`, siret).Scan(&mid); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(mid, 10)
	t.Cleanup(func() { cleanup(merchantID) })
	loc, _ := time.LoadLocation("Europe/Paris")

	register := func(closed bool) string {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO cash_registers (merchant_id, cash_desk_id, device_id, user_id, cash_fund, start_date, closure_comment, closed, end_date)
			VALUES ($1, 1, 'itest-reopen', 'itest', 0, now(), '', $2, CASE WHEN $2 THEN now() END) RETURNING cash_register_id`,
			merchantID, closed).Scan(&id); err != nil {
			t.Fatalf("seed register: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	orderNum := 0
	order := func(state string, deliveredOn time.Time) string {
		t.Helper()
		orderNum++
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, order_source, price, TVA, HT, delivered_on, created_by)
			VALUES ($1, $2, 'WELLO_RESTO', 'CLOSED', $3, 'IN', 'WELLO_RESTO_POS', 1000, 0, 1000, $4, 'itest') RETURNING order_id`,
			merchantID, orderNum, state, deliveredOn).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	payment := func(orderID, registerID string, enabled bool) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, operation_type, enabled, payment_date, cash_register_id)
			VALUES ($1, 'itest', $2, 1000, 'CB', 'SALE', $3, now(), $4)`, merchantID, orderID, enabled, registerID); err != nil {
			t.Fatalf("seed payment: %v", err)
		}
	}
	state := func(orderID string) string {
		t.Helper()
		var s string
		if err := db.QueryRowContext(ctx, `SELECT state FROM orders WHERE order_id = $1`, orderID).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	openReg, closedReg := register(false), register(true)
	now := time.Now()

	// Jour scellé : clôture journalière du 2026-09-10 écrite.
	sealedDay := time.Date(2026, 9, 10, 12, 0, 0, 0, loc)
	sealed := order("CLOSED", sealedDay)
	payment(sealed, openReg, true)
	from := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	if _, err := CloseDueDays(ctx, db, merchantID, "Europe/Paris", &from, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 11, 12, 0, 0, 0, loc)); err != nil {
		t.Fatalf("CloseDueDays: %v", err)
	}

	// Paiement dans un registre fermé, l'autre dans un registre ouvert.
	inClosedReg := order("CLOSED", now)
	payment(inClosedReg, openReg, true)
	payment(inClosedReg, closedReg, true)

	// Paiement annulé dans un registre fermé : ne compte pas.
	cancelledInClosed := order("CLOSED", now)
	payment(cancelledInClosed, closedReg, false)
	payment(cancelledInClosed, openReg, true)

	ok := order("CLOSED", now)
	payment(ok, openReg, true)

	stillOpen := order("OPEN", now)

	// Ce que la caisse affiche : mêmes règles, sans verrou.
	reopenable, err := ReopenableOrders(ctx, dbx.GetDB(ctx, db), []string{sealed, inClosedReg, cancelledInClosed, ok, stillOpen})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{sealed: false, inClosedReg: false, cancelledInClosed: true, ok: true}
	if len(reopenable) != len(want) {
		t.Fatalf("ReopenableOrders = %v, want %v", reopenable, want)
	}
	for id, w := range want {
		if reopenable[id] != w {
			t.Fatalf("ReopenableOrders[%s] = %v, want %v", id, reopenable[id], w)
		}
	}

	if _, err := ReopenOrder(ctx, db, merchantID, sealed); !errors.Is(err, models.ErrReopenOrderSealed) {
		t.Fatalf("sealed = %v, want ErrReopenOrderSealed", err)
	}
	if _, err := ReopenOrder(ctx, db, merchantID, inClosedReg); !errors.Is(err, models.ErrReopenPaymentRegisterClosed) {
		t.Fatalf("closed register = %v, want ErrReopenPaymentRegisterClosed", err)
	}
	if _, err := ReopenOrder(ctx, db, "999999999", ok); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("other merchant = %v, want ErrNotFound", err)
	}
	if state(sealed) != "CLOSED" || state(inClosedReg) != "CLOSED" || state(ok) != "CLOSED" {
		t.Fatal("a refused reopen changed an order")
	}
	for _, id := range []string{cancelledInClosed, ok} {
		if reopened, err := ReopenOrder(ctx, db, merchantID, id); err != nil || !reopened || state(id) != "OPEN" {
			t.Fatalf("reopen %s = (%v, %v), state %s", id, reopened, err, state(id))
		}
	}
	if reopened, err := ReopenOrder(ctx, db, merchantID, stillOpen); err != nil || reopened {
		t.Fatalf("already open = (%v, %v), want no-op", reopened, err)
	}
}
