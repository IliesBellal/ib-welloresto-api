//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/modules/customers"
)

// LOT A Semaine 1, Chantier 2 (docs/decisions.md) : DenyOrderLocal était le
// seul des trois points de clôture de commande (SetDeliveredLocal,
// DeleteOrderLocal, DenyOrderLocal) à ne jamais écrire hash/previous_hash/
// signature — une commande refusée par le marchand sortait de la chaîne
// fiscale sans laisser de trace. Depuis le lot B conformité caisse, les
// commandes sont scellées par leur clôture journalière : vérifie qu'un refus
// est daté (delivered_on), sans empreinte sur la ligne, et qu'il figure avec
// son statut dans la clôture de son jour.
func TestOrderLifeCycleRepository_DenyOrderLocal_SealedByDayClosure_Postgres(t *testing.T) {
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

	orderA := newOpenOrder(t, 3000)
	orderB := newOpenOrder(t, 3001)
	for _, id := range []string{orderA, orderB} {
		if err := repo.DenyOrderLocal(ctx, id, "1", "itest deny "+id, "226"); err != nil {
			t.Fatalf("DenyOrderLocal (%s): %v", id, err)
		}
	}

	var deliveredOn time.Time
	for _, id := range []string{orderA, orderB} {
		var state, status string
		var hashed bool
		if err := db.QueryRowContext(ctx, `SELECT state, brand_status, delivered_on, hash IS NOT NULL FROM orders WHERE order_id = $1`, id).
			Scan(&state, &status, &deliveredOn, &hashed); err != nil {
			t.Fatalf("read back denied order %s: %v", id, err)
		}
		if state != "CLOSED" || status != "DENIED" || hashed {
			t.Fatalf("denied order %s: state=%s status=%s row hash=%v; want CLOSED/DENIED without row hash", id, state, status, hashed)
		}
	}

	// Les deux refus figurent, avec leur statut, dans la clôture de leur jour.
	day := time.Date(deliveredOn.UTC().Year(), deliveredOn.UTC().Month(), deliveredOn.UTC().Day(), 0, 0, 0, 0, time.UTC)
	closure, err := fiscal.ComputeDayClosure(ctx, dbx.GetDB(ctx, db), merchantID, "UTC", time.UTC, day)
	if err != nil {
		t.Fatalf("ComputeDayClosure: %v", err)
	}
	found := map[string]string{}
	for _, o := range closure.Orders {
		found[strconv.FormatInt(o.OrderID, 10)] = o.Status
	}
	if found[orderA] != "DENIED" || found[orderB] != "DENIED" {
		t.Fatalf("denied orders missing from their day closure: %+v", closure.Orders)
	}
}
