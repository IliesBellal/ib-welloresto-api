//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/audit"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/modules/receipt"
	"welloresto-api/internal/utils/dbutils"
)

// Mesure des écritures fiscales (docs/attestation-conformite-01-lot-A-brief.md,
// phases 0 et 5) : temps d'un encaissement et d'une clôture de commande, en
// séquentiel puis avec 10 écritures simultanées sur un même établissement, et
// nombre de fourches dans les chaînes (un même maillon parent utilisé deux
// fois). Test de mesure, lancé seulement avec FISCAL_PERF=1 : il ne vérifie
// rien, il journalise les chiffres (go test -v).
func TestFiscalChainPerf_Postgres(t *testing.T) {
	if os.Getenv("FISCAL_PERF") != "1" {
		t.Skip("FISCAL_PERF=1 not set — skipping fiscal chain measurement")
	}
	db := pgtest.Open(t)
	ctx := context.Background()

	const siret = "siret-fiscal-perf"
	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM receipts WHERE merchant_id = $1`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM audit_logs WHERE merchant_id = $1`,
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
		VALUES ('ITest Fiscal Perf', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mtok-fiscal-perf', 'UTC')
		RETURNING id`, siret).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)

	repo := NewOrdersLifeCycleRepository(db, customers.NewCustomerRepository(db))
	receipts := receipt.NewReceiptService(receipt.NewReceiptRepository(db))
	auditRepo := audit.NewAuditRepository(db)

	orderNum := 5000
	newOrder := func(t *testing.T, price int) string {
		t.Helper()
		orderNum++
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, price, TVA, HT, created_by)
			VALUES ($1, $2, 'WELLO_RESTO', 'PENDING', 'OPEN', $3, 0, $3, 'itest')
			RETURNING order_id`, merchantID, orderNum, price).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	addPayment := func(orderID string, amount int) error {
		return dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
			_, err := repo.AddPaymentAndReturnID(txCtx, models.Payment{
				OrderID: orderID, MerchantID: merchantID, UserID: "itest",
				MOP: "ES", Amount: amount, OperationType: models.OperationTypeSale,
			})
			return err
		})
	}
	closeOrder := func(orderID string, price int64) error {
		return dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
			if _, err := repo.SetDeliveredLocal(txCtx, orderID); err != nil {
				return err
			}
			ht := price
			if err := receipts.GenerateFiscalReceipt(txCtx, &models.Order{
				OrderID: orderID, MerchantID: &merchantID, TTC: price, HT: &ht,
			}, nil, nil); err != nil {
				return err
			}
			return auditRepo.InsertLogWithChain(txCtx, &models.AuditLog{
				ID: fmt.Sprintf("itest-perf-%s-%d", orderID, time.Now().UnixNano()), UserID: "itest",
				MerchantID: merchantID, Action: models.ActionOrderClose, ResourceType: models.ResourceOrder,
				ResourceID: orderID, OldValues: []byte("null"), NewValues: []byte(`{"state":"CLOSED"}`),
			})
		})
	}

	// 0. Aller-retour réseau seul (SELECT 1) : chaque requête d'un chemin le
	// paie une fois. Depuis un poste distant, il domine les mesures ; entre
	// l'API et la base hébergées ensemble, il est inférieur à la milliseconde.
	rtt := make([]time.Duration, 0, 30)
	for i := 0; i < 30; i++ {
		start := time.Now()
		if _, err := db.ExecContext(ctx, `SELECT 1`); err != nil {
			t.Fatalf("SELECT 1: %v", err)
		}
		rtt = append(rtt, time.Since(start))
	}
	logDurations(t, "aller-retour réseau (SELECT 1, 30)", rtt)

	// 1. Encaissements séquentiels.
	bigOrder := newOrder(t, 1_000_000)
	seq := make([]time.Duration, 0, 30)
	for i := 0; i < 30; i++ {
		start := time.Now()
		if err := addPayment(bigOrder, 1); err != nil {
			t.Fatalf("sequential payment: %v", err)
		}
		seq = append(seq, time.Since(start))
	}
	logDurations(t, "encaissement séquentiel (30)", seq)

	// 2. 10 encaissements simultanés sur le même établissement.
	conc, wall, errs := runConcurrent(10, func(int) error { return addPayment(bigOrder, 1) })
	logDurations(t, "encaissement simultané (10)", conc)
	t.Logf("encaissement simultané : durée totale %v, erreurs %d", wall, errs)

	// 3. Clôtures séquentielles (commande payée, clôture + ticket + audit).
	const price = 1500
	prepare := func(n int) []string {
		ids := make([]string, n)
		for i := range ids {
			ids[i] = newOrder(t, price)
			if err := addPayment(ids[i], price); err != nil {
				t.Fatalf("seed payment: %v", err)
			}
		}
		return ids
	}
	closeSeq := make([]time.Duration, 0, 20)
	for _, id := range prepare(20) {
		start := time.Now()
		if err := closeOrder(id, price); err != nil {
			t.Fatalf("sequential close: %v", err)
		}
		closeSeq = append(closeSeq, time.Since(start))
	}
	logDurations(t, "clôture séquentielle (20)", closeSeq)

	// 4. 10 clôtures simultanées sur le même établissement.
	ids := prepare(10)
	closeConc, closeWall, closeErrs := runConcurrent(10, func(i int) error { return closeOrder(ids[i], price) })
	logDurations(t, "clôture simultanée (10)", closeConc)
	t.Logf("clôture simultanée : durée totale %v, erreurs %d", closeWall, closeErrs)

	// 5. Fourches et numéros de ticket en double pour cet établissement.
	forks := func(query string) int {
		var n int
		if err := db.QueryRowContext(ctx, query, merchantID).Scan(&n); err != nil {
			t.Fatalf("fork count: %v", err)
		}
		return n
	}
	t.Logf("fourches payments : %d", forks(`SELECT count(*) - count(DISTINCT previous_hash) FROM payments WHERE merchant_id = $1 AND previous_hash <> ''`))
	t.Logf("fourches orders : %d", forks(`SELECT count(*) - count(DISTINCT previous_hash) FROM orders WHERE merchant_id = $1 AND previous_hash IS NOT NULL AND previous_hash <> ''`))
	t.Logf("fourches receipts : %d", forks(`SELECT count(*) - count(DISTINCT prev_hash) FROM receipts WHERE merchant_id = $1 AND prev_hash <> ''`))
	t.Logf("fourches audit_logs : %d", forks(`SELECT count(*) - count(DISTINCT previous_hash) FROM audit_logs WHERE merchant_id = $1 AND previous_hash <> ''`))
	t.Logf("numéros de ticket en double : %d", forks(`SELECT count(*) - count(DISTINCT receipt_number) FROM receipts WHERE merchant_id = $1`))
}

func runConcurrent(n int, fn func(i int) error) ([]time.Duration, time.Duration, int) {
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		durs  []time.Duration
		errs  int
		start = make(chan struct{})
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			t0 := time.Now()
			err := fn(i)
			d := time.Since(t0)
			mu.Lock()
			durs = append(durs, d)
			if err != nil {
				errs++
			}
			mu.Unlock()
		}(i)
	}
	t0 := time.Now()
	close(start)
	wg.Wait()
	return durs, time.Since(t0), errs
}

func logDurations(t *testing.T, label string, d []time.Duration) {
	t.Helper()
	if len(d) == 0 {
		return
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	pct := func(p float64) time.Duration { return s[int(p*float64(len(s)-1))] }
	t.Logf("%s : p50 %v, p95 %v, max %v", label, pct(0.50).Round(time.Millisecond), pct(0.95).Round(time.Millisecond), s[len(s)-1].Round(time.Millisecond))
}
