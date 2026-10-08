//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/modules/receipt"
	"welloresto-api/internal/utils/dbutils"
)

// Mesure de la clôture, de la réouverture et de la reclôture d'une commande
// (lot C conformité caisse, phase 2). Test de mesure, lancé seulement avec
// FISCAL_PERF=1 : il ne vérifie rien, il journalise les chiffres (go test -v).
func TestReopenPerf_Postgres(t *testing.T) {
	if os.Getenv("FISCAL_PERF") != "1" {
		t.Skip("FISCAL_PERF=1 not set — skipping reopen measurement")
	}
	db := pgtest.Open(t)
	ctx := context.Background()

	const siret = "siret-reopen-perf"
	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM receipts WHERE merchant_id = $1`,
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
	t.Cleanup(func() { cleanupFor(merchantID) })
	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Reopen Perf', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mtok-reopen-perf', 'UTC')
		RETURNING id`, siret).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)

	repo := NewOrdersLifeCycleRepository(db, customers.NewCustomerRepository(db))
	receipts := receipt.NewReceiptService(receipt.NewReceiptRepository(db))
	const price = 1500
	orderNum := 9000
	newPaidOrder := func() string {
		orderNum++
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, price, TVA, HT, created_by)
			VALUES ($1, $2, 'WELLO_RESTO', 'PENDING', 'OPEN', $3, 0, $3, 'itest') RETURNING order_id`,
			merchantID, orderNum, price).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, operation_type, enabled, payment_date)
			VALUES ($1, 'itest', $2, $3, 'ES', 'SALE', TRUE, now())`, merchantID, id, price); err != nil {
			t.Fatalf("seed payment: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	closeOrder := func(orderID string) error {
		return dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
			if _, err := repo.SetDeliveredLocal(txCtx, orderID); err != nil {
				return err
			}
			ht := int64(price)
			return receipts.GenerateFiscalReceipt(txCtx, &models.Order{OrderID: orderID, MerchantID: &merchantID, TTC: price, HT: &ht}, nil)
		})
	}
	reopen := func(orderID string) error {
		return dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
			return repo.ReopenClosedOrder(txCtx, merchantID, orderID, "itest")
		})
	}
	measure := func(label string, ids []string, fn func(string) error) {
		d := make([]time.Duration, 0, len(ids))
		for _, id := range ids {
			start := time.Now()
			if err := fn(id); err != nil {
				t.Fatalf("%s: %v", label, err)
			}
			d = append(d, time.Since(start))
		}
		logDurations(t, label, d)
	}

	ids := make([]string, 20)
	for i := range ids {
		ids[i] = newPaidOrder()
	}
	measure("clôture (20)", ids, closeOrder)
	measure("réouverture (20)", ids, reopen)
	measure("reclôture à l'identique (20)", ids, closeOrder)
	var n int
	_ = db.QueryRowContext(ctx, `SELECT count(*) FROM receipts WHERE merchant_id = $1`, merchantID).Scan(&n)
	t.Logf("tickets écrits : %d", n)
}
