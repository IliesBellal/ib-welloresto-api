//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/modules/customers"
)

// TestCustomerStats_Postgres exercises PROMPT 26's fix end to end against a
// real Postgres: a canceled-after-delivery order must give back what it
// added (cause #1, the dominant driver of the 29%/50% drift measured on
// PROD), a reopen-then-reclose must not double-count (cause #2, confirmed on
// staging via 39 orders closed twice), and a non-WELLO_RESTO order must now
// count at all (cause #3 — customer_nb_orders was WELLO_RESTO-only before
// this lot).
func TestCustomerStats_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM customer WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-olc-stats' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest OLC Stats', 'a', '1', 's', '75001', 'Paris', 'siret-olc-stats', 'https://x', '06', 'mtok-olc-stats', 'UTC')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)

	custoRepo := customers.NewCustomerRepository(db)
	repo := NewOrdersLifeCycleRepository(db, custoRepo)

	var customerIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO customer (merchant_id, customer_name, customer_brand)
		VALUES ($1, 'itest stats client', 'WELLO_RESTO') RETURNING customer_id`, merchantID).Scan(&customerIntID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	customerID := strconv.FormatInt(customerIntID, 10)

	readStats := func(t *testing.T) (nbOrders, totalSpent int, lastOrderDate sql.NullTime) {
		t.Helper()
		if err := db.QueryRowContext(ctx, `
			SELECT customer_nb_orders, customer_total_spent, last_order_date
			FROM customer WHERE customer_id = $1`, customerID).Scan(&nbOrders, &totalSpent, &lastOrderDate); err != nil {
			t.Fatalf("read back customer stats: %v", err)
		}
		return
	}

	newOrder := func(t *testing.T, brand string, price int, orderNum int, creationDate time.Time) string {
		t.Helper()
		var orderID int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, customer_id, order_num, brand, brand_status, order_type, state, price, TVA, HT, created_by, creation_date)
			VALUES ($1, $2, $3, $4, 'ACCEPTED', 'IN', 'OPEN', $5, 0, $5, 'itest', $6)
			RETURNING order_id`, merchantID, customerID, orderNum, brand, price, creationDate).Scan(&orderID); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		return strconv.FormatInt(orderID, 10)
	}

	closeOrder := func(t *testing.T, orderID string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `UPDATE orders SET state = 'CLOSED' WHERE order_id = $1`, orderID); err != nil {
			t.Fatalf("close order: %v", err)
		}
	}

	day1 := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)

	// --- Cross-channel (cause #3) : une commande Uber Eats compte désormais ---
	orderUE := newOrder(t, "UBER_EATS", 1500, 1, day1)
	closeOrder(t, orderUE)
	if err := custoRepo.ApplyOrderToCustomerStats(ctx, orderUE); err != nil {
		t.Fatalf("ApplyOrderToCustomerStats (Uber Eats): %v", err)
	}
	nbOrders, totalSpent, lastOrderDate := readStats(t)
	if nbOrders != 1 || totalSpent != 1500 {
		t.Fatalf("expected 1 order / 1500 after Uber Eats closure, got %d / %d", nbOrders, totalSpent)
	}
	if !lastOrderDate.Valid || !lastOrderDate.Time.Equal(day1) {
		t.Fatalf("expected last_order_date=%v, got %v", day1, lastOrderDate)
	}

	// --- Idempotence (cause #2, la voie directe) : appliquer deux fois la
	// même commande ne compte qu'une fois — couvre le cas SetDeliveredExternal
	// rejouée après une clôture manuelle déjà effectuée.
	if err := custoRepo.ApplyOrderToCustomerStats(ctx, orderUE); err != nil {
		t.Fatalf("ApplyOrderToCustomerStats (repeat): %v", err)
	}
	nbOrders, totalSpent, _ = readStats(t)
	if nbOrders != 1 || totalSpent != 1500 {
		t.Fatalf("expected stats unchanged on repeat, got %d / %d", nbOrders, totalSpent)
	}

	// --- Second order, plus récent, WELLO_RESTO ---
	orderWR := newOrder(t, "WELLO_RESTO", 2000, 2, day2)
	closeOrder(t, orderWR)
	if err := custoRepo.ApplyOrderToCustomerStats(ctx, orderWR); err != nil {
		t.Fatalf("ApplyOrderToCustomerStats (WELLO_RESTO): %v", err)
	}
	nbOrders, totalSpent, lastOrderDate = readStats(t)
	if nbOrders != 2 || totalSpent != 3500 {
		t.Fatalf("expected 2 orders / 3500 after second closure, got %d / %d", nbOrders, totalSpent)
	}
	if !lastOrderDate.Valid || !lastOrderDate.Time.Equal(day2) {
		t.Fatalf("expected last_order_date=%v (most recent), got %v", day2, lastOrderDate)
	}

	// --- Annulation après clôture (cause #1, la cause dominante) : annuler la
	// commande la plus récente doit retirer son montant ET faire réapparaître
	// la date de la commande précédente, jamais la laisser périmée.
	if err := repo.DeleteOrderLocal(ctx, orderWR, "1", "itest cancel after delivery", "226"); err != nil {
		t.Fatalf("DeleteOrderLocal: %v", err)
	}
	nbOrders, totalSpent, lastOrderDate = readStats(t)
	if nbOrders != 1 || totalSpent != 1500 {
		t.Fatalf("expected stats reverted to 1 order / 1500 after cancel-after-delivery, got %d / %d", nbOrders, totalSpent)
	}
	if !lastOrderDate.Valid || !lastOrderDate.Time.Equal(day1) {
		t.Fatalf("expected last_order_date reverted to previous order %v, got %v", day1, lastOrderDate)
	}

	// Annuler une seconde fois la même commande (rejeu / double-clic) ne doit
	// rien retirer de plus : elle n'est déjà plus comptée.
	if err := repo.DeleteOrderLocal(ctx, orderWR, "1", "itest double cancel", "226"); err != nil {
		t.Fatalf("DeleteOrderLocal (repeat): %v", err)
	}
	nbOrders, totalSpent, _ = readStats(t)
	if nbOrders != 1 || totalSpent != 1500 {
		t.Fatalf("expected stats unchanged on repeat cancel, got %d / %d", nbOrders, totalSpent)
	}

	// --- Réouverture pour correction, puis reclôture (cause #2, la voie
	// staff) : le prix corrigé doit remplacer l'ancien, jamais s'y ajouter.
	if err := repo.ReopenClosedOrder(ctx, merchantID, orderUE, "226"); err != nil {
		t.Fatalf("ReopenClosedOrder: %v", err)
	}
	nbOrders, totalSpent, lastOrderDate = readStats(t)
	if nbOrders != 0 || totalSpent != 0 {
		t.Fatalf("expected stats reverted to 0 after reopening the only counted order, got %d / %d", nbOrders, totalSpent)
	}
	if lastOrderDate.Valid {
		t.Fatalf("expected last_order_date NULL with no counted order left, got %v", lastOrderDate)
	}

	if _, err := db.ExecContext(ctx, `UPDATE orders SET price = 1800, state = 'CLOSED' WHERE order_id = $1`, orderUE); err != nil {
		t.Fatalf("correct price before reclose: %v", err)
	}
	if err := custoRepo.ApplyOrderToCustomerStats(ctx, orderUE); err != nil {
		t.Fatalf("ApplyOrderToCustomerStats (reclose): %v", err)
	}
	nbOrders, totalSpent, _ = readStats(t)
	if nbOrders != 1 || totalSpent != 1800 {
		t.Fatalf("expected 1 order / corrected 1800 after reclose, got %d / %d", nbOrders, totalSpent)
	}
}
