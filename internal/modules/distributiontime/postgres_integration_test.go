//go:build postgres_integration

package distributiontime

import (
	"context"
	"fmt"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Vérifie la traduction directe de GET_AVERAGE_DISTRIBUTION_TIME contre le
// Postgres de dev, en comparant au calcul manuel :
//
//	temps = ROUND(LEAST(GREATEST((pending + nb) * LEAST(adt, 180) / capacity, min), max))
//
// où pending = SUM(quantity - distributed_quantity) des orderitems non
// distribués des commandes OPEN (non planifiées, ou planifiées prêtes dans
// moins de 90 min).
func TestEstimatedSeconds_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM orderitems WHERE merchant_id LIKE 'itest-adt-%'`,
			`DELETE FROM orders WHERE merchant_id LIKE 'itest-adt-%'`,
			`DELETE FROM merchant_parameters WHERE merchant_id LIKE 'itest-adt-%'`,
			`DELETE FROM average_distribution_time WHERE merchant_id LIKE 'itest-adt-%'`,
		} {
			_, _ = db.ExecContext(ctx, q)
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	seedMerchant := func(merchantID string, adtSeconds, capacity, minPrep, maxPrep int) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO average_distribution_time (merchant_id, distribution_time)
			VALUES ($1, $2)`, merchantID, adtSeconds); err != nil {
			t.Fatalf("seed average_distribution_time %s: %v", merchantID, err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO merchant_parameters (merchant_id, last_menu_update, concurrent_preparation_capacity, minimum_preparation_time, maximum_preparation_time)
			VALUES ($1, now(), $2, $3, $4)`, merchantID, capacity, minPrep, maxPrep); err != nil {
			t.Fatalf("seed merchant_parameters %s: %v", merchantID, err)
		}
	}

	// readyExpr : expression SQL pour estimated_ready (ex. "now() + interval '30 minutes'" ou "NULL")
	insertOrder := func(merchantID, state string, scheduled bool, readyExpr string) int {
		t.Helper()
		var id int
		query := fmt.Sprintf(`
			INSERT INTO orders (merchant_id, order_num, brand_status, state, scheduled, price, TVA, HT, created_by, estimated_ready)
			VALUES ($1, 1, 'ACCEPTED', $2, $3, 0, 0, 0, 'itest', %s)
			RETURNING order_id`, readyExpr)
		if err := db.QueryRowContext(ctx, query, merchantID, state, scheduled).Scan(&id); err != nil {
			t.Fatalf("seed order %s: %v", merchantID, err)
		}
		return id
	}

	insertItem := func(merchantID string, orderID, qty, distributedQty int, isDistributed bool) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, distributed_quantity, isDistributed, price)
			VALUES ($1, 1, $2, $3, $4, $5, 0)`, orderID, merchantID, qty, distributedQty, isDistributed); err != nil {
			t.Fatalf("seed orderitem order %d: %v", orderID, err)
		}
	}

	// --- Cas nominal : adt=200 (plafonné à 180), capacity=2, bornes [300, 3600] ---
	main := "itest-adt-main"
	seedMerchant(main, 200, 2, 300, 3600)

	// Commande OPEN non planifiée : 5-1=4 pending ; l'item déjà distribué (7) est exclu.
	o1 := insertOrder(main, "OPEN", false, "NULL")
	insertItem(main, o1, 5, 1, false)
	insertItem(main, o1, 7, 7, true)

	// Planifiée prête dans 30 min (< 90) : comptée (+2).
	o2 := insertOrder(main, "OPEN", true, "now() + interval '30 minutes'")
	insertItem(main, o2, 2, 0, false)

	// Planifiée prête dans 5 h (> 90 min) : exclue.
	o3 := insertOrder(main, "OPEN", true, "now() + interval '5 hours'")
	insertItem(main, o3, 50, 0, false)

	// Commande non OPEN : exclue.
	o4 := insertOrder(main, "DONE", false, "NULL")
	insertItem(main, o4, 50, 0, false)

	// Attendu à la main : (4 + 2 + 3) * LEAST(200,180) / 2 = 9*180/2 = 810, dans [300,3600].
	sec, found, err := EstimatedSeconds(ctx, db, main, 3)
	if err != nil {
		t.Fatalf("EstimatedSeconds(main): %v", err)
	}
	if !found || sec != 810 {
		t.Fatalf("main: got (sec=%d, found=%v), want (810, true)", sec, found)
	}

	// --- Borne basse : (0+2)*120/1 = 240 < min 300 → 300 ---
	minClamp := "itest-adt-minclamp"
	seedMerchant(minClamp, 120, 1, 300, 3600)
	sec, found, err = EstimatedSeconds(ctx, db, minClamp, 2)
	if err != nil {
		t.Fatalf("EstimatedSeconds(minclamp): %v", err)
	}
	if !found || sec != 300 {
		t.Fatalf("minclamp: got (sec=%d, found=%v), want (300, true)", sec, found)
	}

	// --- Borne haute : (0+10)*LEAST(3000,180)/1 = 1800 > max 600 → 600 ---
	maxClamp := "itest-adt-maxclamp"
	seedMerchant(maxClamp, 3000, 1, 60, 600)
	sec, found, err = EstimatedSeconds(ctx, db, maxClamp, 10)
	if err != nil {
		t.Fatalf("EstimatedSeconds(maxclamp): %v", err)
	}
	if !found || sec != 600 {
		t.Fatalf("maxclamp: got (sec=%d, found=%v), want (600, true)", sec, found)
	}

	// --- capacity=0 : MySQL renvoyait NULL (÷0) rattrapé par IFNULL → min.
	// En Postgres le NULLIF évite l'erreur division_by_zero → même résultat.
	zeroCap := "itest-adt-zerocap"
	seedMerchant(zeroCap, 180, 0, 300, 3600)
	sec, found, err = EstimatedSeconds(ctx, db, zeroCap, 5)
	if err != nil {
		t.Fatalf("EstimatedSeconds(zerocap): %v", err)
	}
	if !found || sec != 300 {
		t.Fatalf("zerocap: got (sec=%d, found=%v), want (300, true)", sec, found)
	}

	// --- Merchant inconnu : aucune ligne, comme la procédure ---
	sec, found, err = EstimatedSeconds(ctx, db, "itest-adt-unknown", 3)
	if err != nil {
		t.Fatalf("EstimatedSeconds(unknown): %v", err)
	}
	if found || sec != 0 {
		t.Fatalf("unknown: got (sec=%d, found=%v), want (0, false)", sec, found)
	}

}
