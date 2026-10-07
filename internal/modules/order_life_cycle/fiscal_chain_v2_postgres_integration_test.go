//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/audit"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/modules/receipt"
	"welloresto-api/internal/utils/dbutils"
)

// Lot A conformité caisse (docs/attestation-conformite-01-lot-A-brief.md) :
// chaque ligne écrite par les chaînes payments, receipts et
// audit_logs doit pouvoir être re-scellée à partir de sa relecture en base
// (même empreinte, même signature), et chaque chaîne doit rester linéaire,
// y compris avec 10 écritures simultanées sur un même établissement.
func TestFiscalChainV2_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	env := newFiscalTestEnv(t, db, "siret-fv2")

	// 1. Une vente complète : lignes, deux paiements, clôture, ticket, audit,
	// puis un avoir ; un refus et une annulation.
	sale := env.newOrder(t, 1500)
	env.addLine(t, sale, 2, 600, 5.5)
	env.addLine(t, sale, 1, 300, 20)
	comment := "Réglé en deux fois"
	env.pay(t, sale, 1000, "ES", &comment)
	env.pay(t, sale, 500, "CB", nil)
	env.close(t, sale, 1500)
	if err := dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
		original, err := env.receipts.GetSaleReceiptByOrderID(txCtx, sale)
		if err != nil {
			return err
		}
		return env.receipts.GenerateRefundReceipt(txCtx, env.merchantID, sale, original, -300, "ES")
	}); err != nil {
		t.Fatalf("refund receipt: %v", err)
	}
	if err := env.repo.DenyOrderLocal(ctx, env.newOrder(t, 800), "1", "itest deny v2", "226"); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if err := env.repo.DeleteOrderLocal(ctx, env.newOrder(t, 900), "1", "itest delete v2", "226"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	env.verifyAll(t)

	// 2. 10 encaissements puis 10 clôtures simultanés : chaînes linéaires,
	// numéros de ticket distincts et continus.
	big := env.newOrder(t, 1_000_000)
	if _, _, errs := runConcurrent(10, func(int) error { return env.payErr(big, 1, "ES", nil) }); errs != 0 {
		t.Fatalf("concurrent payments: %d errors", errs)
	}
	ids := make([]string, 10)
	for i := range ids {
		ids[i] = env.newOrder(t, 1200)
		env.pay(t, ids[i], 1200, "CB", nil)
	}
	if _, _, errs := runConcurrent(10, func(i int) error { return env.closeErr(ids[i], 1200) }); errs != 0 {
		t.Fatalf("concurrent closures: %d errors", errs)
	}
	env.verifyAll(t)
	env.verifyReceiptNumbersContiguous(t)
}

// Le verrou est propre à un établissement : le tenir pour l'un ne doit pas
// retarder les écritures fiscales d'un autre.
func TestFiscalLock_DoesNotBlockOtherMerchants_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	a := newFiscalTestEnv(t, db, "siret-fl-a")
	b := newFiscalTestEnv(t, db, "siret-fl-b")
	orderB := b.newOrder(t, 1000)

	locked, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
			if err := fiscal.LockChain(txCtx, fiscal.ChainPayments, a.merchantID); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Errorf("holder tx: %v", err)
		}
	}()

	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := dbutils.RunInTx(wctx, db, func(txCtx context.Context) error {
		_, err := b.repo.AddPaymentAndReturnID(txCtx, models.Payment{
			OrderID: orderB, MerchantID: b.merchantID, UserID: "itest", MOP: "ES", Amount: 1000, OperationType: models.OperationTypeSale,
		})
		return err
	}); err != nil {
		t.Fatalf("payment for merchant B while merchant A is locked: %v", err)
	}
}

// Une mutation de commande tient la ligne de la commande puis écrit son audit
// (ExecuteOrderMutation, UpdateOrder) pendant qu'un encaissement sur la même
// commande tient sa chaîne puis met la commande à jour (isPaid). Avec un
// verrou unique par établissement, les deux s'attendaient mutuellement
// (interblocage, une transaction annulée) ; avec un verrou par chaîne, les
// deux aboutissent.
func TestFiscalLock_NoDeadlockBetweenOrderRowAndChains_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	env := newFiscalTestEnv(t, db, "siret-fdl")
	order := env.newOrder(t, 1000)

	rowLocked, writeAudit := make(chan struct{}), make(chan struct{})
	mutation, payment := make(chan error, 1), make(chan error, 1)
	go func() {
		mutation <- dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
			if _, err := dbx.GetDB(txCtx, db).ExecContext(txCtx, `SELECT 1 FROM orders WHERE order_id = ? FOR UPDATE`, order); err != nil {
				return err
			}
			close(rowLocked)
			<-writeAudit
			return env.audit.InsertLogWithChain(txCtx, &models.AuditLog{
				ID: fmt.Sprintf("itest-fdl-%d", time.Now().UnixNano()), UserID: "itest", MerchantID: env.merchantID,
				Action: models.ActionOrderUpdate, ResourceType: models.ResourceOrder, ResourceID: order,
				OldValues: json.RawMessage(`null`), NewValues: json.RawMessage(`{"x":1}`),
			})
		})
	}()
	<-rowLocked
	go func() { payment <- env.payErr(order, 1000, "ES", nil) }()
	time.Sleep(500 * time.Millisecond) // l'encaissement tient sa chaîne et attend la ligne
	close(writeAudit)

	timeout := time.After(15 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case err := <-mutation:
			if err != nil {
				t.Fatalf("order mutation: %v", err)
			}
		case err := <-payment:
			if err != nil {
				t.Fatalf("payment: %v", err)
			}
		case <-timeout:
			t.Fatal("order mutation and payment did not both complete")
		}
	}
}

// Lot A conformité caisse (C10) : la confirmation de livraison Uber arrive
// souvent après la clôture en caisse. Elle ne doit ni reclôturer la commande
// (date de clôture réécrite) ni émettre un second ticket.
func TestSetDeliveredExternal_AlreadyClosed_NoOp_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	env := newFiscalTestEnv(t, db, "siret-fext")
	order := env.newOrder(t, 1200)
	env.pay(t, order, 1200, "CB", nil)
	env.close(t, order, 1200)

	snapshot := func() (lastUpdate, closedAt time.Time, receipts int) {
		t.Helper()
		if err := db.QueryRowContext(ctx, `
			SELECT o.last_update, o.delivered_on, (SELECT count(*) FROM receipts r WHERE r.order_id = o.order_id)
			FROM orders o WHERE o.order_id = $1`, order).Scan(&lastUpdate, &closedAt, &receipts); err != nil {
			t.Fatalf("read order: %v", err)
		}
		return
	}
	updatedBefore, closedBefore, receiptsBefore := snapshot()

	// Seul le dépôt sert avant le retour anticipé sur une commande close.
	svc := &OrdersLifeCycleService{ordersLifeCycleRepo: env.repo}
	if err := svc.SetDeliveredExternal(ctx, env.merchantID, models.UberEatsWebhookUserID, order); err != nil {
		t.Fatalf("SetDeliveredExternal on a closed order: %v", err)
	}
	updatedAfter, closedAfter, receiptsAfter := snapshot()
	if !updatedAfter.Equal(updatedBefore) || !closedAfter.Equal(closedBefore) || receiptsAfter != receiptsBefore || receiptsBefore != 1 {
		t.Fatalf("closed order changed: last_update %v→%v, closed_at %v→%v, receipts %d→%d",
			updatedBefore, updatedAfter, closedBefore, closedAfter, receiptsBefore, receiptsAfter)
	}
}

type fiscalTestEnv struct {
	db         *sql.DB
	merchantID string
	repo       *OrdersLifeCycleRepository
	receipts   receipt.ReceiptService
	audit      audit.AuditRepository
	orderNum   int
}

func newFiscalTestEnv(t *testing.T, db *sql.DB, siret string) *fiscalTestEnv {
	t.Helper()
	ctx := context.Background()
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM receipts WHERE merchant_id = $1`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM audit_logs WHERE merchant_id = $1`,
			`DELETE FROM orderitems WHERE merchant_id = $1`,
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
	var id int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Fiscal', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', $2, 'UTC')
		RETURNING id`, siret, "mtok-"+siret).Scan(&id); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	env := &fiscalTestEnv{
		db:         db,
		merchantID: strconv.FormatInt(id, 10),
		repo:       NewOrdersLifeCycleRepository(db, customers.NewCustomerRepository(db)),
		receipts:   receipt.NewReceiptService(receipt.NewReceiptRepository(db)),
		audit:      audit.NewAuditRepository(db),
		orderNum:   7000,
	}
	t.Cleanup(func() { cleanupFor(env.merchantID) })
	return env
}

func (e *fiscalTestEnv) newOrder(t *testing.T, price int) string {
	t.Helper()
	e.orderNum++
	var id int64
	if err := e.db.QueryRowContext(context.Background(), `
		INSERT INTO orders (merchant_id, order_num, brand, brand_status, state, order_type, price, TVA, HT, created_by)
		VALUES ($1, $2, 'WELLO_RESTO', 'PENDING', 'OPEN', 'IN', $3, 0, $3, 'itest')
		RETURNING order_id`, e.merchantID, e.orderNum, price).Scan(&id); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	return strconv.FormatInt(id, 10)
}

func (e *fiscalTestEnv) addLine(t *testing.T, orderID string, qty, price int, tvaRate float64) {
	t.Helper()
	if _, err := e.db.ExecContext(context.Background(), `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, base_price, price, tva_rate)
		VALUES ($1, 1, $2, $3, $4, $4, $5)`, orderID, e.merchantID, qty, price, tvaRate); err != nil {
		t.Fatalf("seed order line: %v", err)
	}
}

func (e *fiscalTestEnv) payErr(orderID string, amount int, mop string, comment *string) error {
	_, err := e.repo.AddPaymentAndReturnID(context.Background(), models.Payment{
		OrderID: orderID, MerchantID: e.merchantID, UserID: "itest", MOP: mop, Amount: amount,
		OperationType: models.OperationTypeSale, Comment: comment,
	})
	return err
}

func (e *fiscalTestEnv) pay(t *testing.T, orderID string, amount int, mop string, comment *string) {
	t.Helper()
	if err := e.payErr(orderID, amount, mop, comment); err != nil {
		t.Fatalf("payment: %v", err)
	}
}

// closeErr reproduit DeliverOrder (clôture + ticket) et l'audit
// d'ExecuteOrderMutation, dans une même transaction.
func (e *fiscalTestEnv) closeErr(orderID string, price int64) error {
	return dbutils.RunInTx(context.Background(), e.db, func(txCtx context.Context) error {
		if _, err := e.repo.SetDeliveredLocal(txCtx, orderID); err != nil {
			return err
		}
		ht := price * 100 / 110
		items := []models.SnapshotItem{{Name: "Café crème « maison »", Quantity: 2, PriceTTC: price / 2, TaxRate: 1000, TaxAmount: 45}}
		payments := []models.SnapshotPayment{{Amount: int(price), MOP: "CB"}}
		if err := e.receipts.GenerateFiscalReceipt(txCtx, &models.Order{OrderID: orderID, MerchantID: &e.merchantID, TTC: price, HT: &ht}, items, payments); err != nil {
			return err
		}
		return e.audit.InsertLogWithChain(txCtx, &models.AuditLog{
			ID: fmt.Sprintf("itest-v2-%s-%d", orderID, time.Now().UnixNano()), UserID: "itest", MerchantID: e.merchantID,
			Action: models.ActionOrderClose, ResourceType: models.ResourceOrder, ResourceID: orderID,
			OldValues: json.RawMessage(`{"state": "OPEN", "price": 12.50, "ratio": 1e-06}`),
			NewValues: json.RawMessage(`{"state":"CLOSED","name":"Crème <brûlée> & co"}`),
		})
	})
}

func (e *fiscalTestEnv) close(t *testing.T, orderID string, price int64) {
	t.Helper()
	if err := e.closeErr(orderID, price); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// chainRow est une ligne relue : son parent, son empreinte, sa signature, et
// l'empreinte recalculée à partir de la relecture.
type chainRow struct {
	key                         string
	prev, hash, sig, recomputed string
	recomputedSig               string
	version                     int
}

func (e *fiscalTestEnv) verifyAll(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	var rows []chainRow
	// payments
	rows = rows[:0]
	pr, err := e.db.QueryContext(ctx, `
		SELECT payment_id, merchant_id, order_id::text, amount, mop, operation_type, payment_date, user_id, comment,
		       previous_hash, hash, signature, hash_version
		FROM payments WHERE merchant_id = $1 ORDER BY payment_date, payment_id`, e.merchantID)
	if err != nil {
		t.Fatal(err)
	}
	for pr.Next() {
		var id int64
		var merchant, order, mop, op, user string
		var amount int
		var date time.Time
		var comment sql.NullString
		var r chainRow
		if err := pr.Scan(&id, &merchant, &order, &amount, &mop, &op, &date, &user, &comment, &r.prev, &r.hash, &r.sig, &r.version); err != nil {
			t.Fatal(err)
		}
		var c *string
		if comment.Valid {
			c = &comment.String
		}
		p, err := fiscal.NewPaymentPayload(merchant, order, amount, mop, op, date, user, c)
		if err != nil {
			t.Fatal(err)
		}
		r.key = "payment " + strconv.FormatInt(id, 10)
		r.recomputed, r.recomputedSig, _ = fiscal.Seal(fiscal.ChainPayments, r.prev, p)
		rows = append(rows, r)
	}
	pr.Close()
	checkChain(t, "payments", rows)

	// orders : plus d'empreinte par commande (scellées par leur clôture
	// journalière, lot B) ; chaque clôture porte sa date de clôture.
	var closedOrders, sealedOnRow, undated int
	if err := e.db.QueryRowContext(ctx, `
		SELECT count(*), count(*) FILTER (WHERE hash IS NOT NULL), count(*) FILTER (WHERE delivered_on IS NULL)
		FROM orders WHERE merchant_id = $1 AND state = 'CLOSED'`, e.merchantID).Scan(&closedOrders, &sealedOnRow, &undated); err != nil {
		t.Fatal(err)
	}
	if closedOrders == 0 || sealedOnRow != 0 || undated != 0 {
		t.Fatalf("orders: %d closed, %d sealed on their row (want 0), %d without delivered_on (want 0)", closedOrders, sealedOnRow, undated)
	}

	// receipts
	rows = nil
	rr, err := e.db.QueryContext(ctx, `
		SELECT merchant_id, receipt_number, order_id::text, created_at, total_ttc, total_ht,
		       tax_details::text, items_snapshot::text, payments_snapshot::text, prev_hash, hash, signature, hash_version
		FROM receipts WHERE merchant_id = $1 ORDER BY created_at, receipt_number`, e.merchantID)
	if err != nil {
		t.Fatal(err)
	}
	for rr.Next() {
		var merchant, number, order, tax, items, pays string
		var at time.Time
		var ttc, ht int
		var r chainRow
		if err := rr.Scan(&merchant, &number, &order, &at, &ttc, &ht, &tax, &items, &pays, &r.prev, &r.hash, &r.sig, &r.version); err != nil {
			t.Fatal(err)
		}
		p, err := fiscal.NewReceiptPayload(merchant, number, order, at, ttc, ht, []byte(tax), []byte(items), []byte(pays))
		if err != nil {
			t.Fatal(err)
		}
		r.key = "receipt " + number
		r.recomputed, r.recomputedSig, _ = fiscal.Seal(fiscal.ChainReceipts, r.prev, p)
		rows = append(rows, r)
	}
	rr.Close()
	checkChain(t, "receipts", rows)

	// audit_logs
	rows = nil
	ar, err := e.db.QueryContext(ctx, `
		SELECT id, merchant_id, user_id, action, resource_type, resource_id, created_at,
		       old_values::text, new_values::text, previous_hash, hash, signature, hash_version
		FROM audit_logs WHERE merchant_id = $1 ORDER BY created_at, id`, e.merchantID)
	if err != nil {
		t.Fatal(err)
	}
	for ar.Next() {
		var id, merchant, user, action, rtype, rid string
		var at time.Time
		var oldV, newV, sig sql.NullString
		var r chainRow
		if err := ar.Scan(&id, &merchant, &user, &action, &rtype, &rid, &at, &oldV, &newV, &r.prev, &r.hash, &sig, &r.version); err != nil {
			t.Fatal(err)
		}
		r.sig = sig.String
		p, err := fiscal.NewAuditLogPayload(id, merchant, user, action, rtype, rid, at, nullBytes(oldV), nullBytes(newV))
		if err != nil {
			t.Fatal(err)
		}
		r.key = "audit " + id
		r.recomputed, r.recomputedSig, _ = fiscal.Seal(fiscal.ChainAuditLogs, r.prev, p)
		rows = append(rows, r)
	}
	ar.Close()
	checkChain(t, "audit_logs", rows)
}

func nullBytes(s sql.NullString) []byte {
	if !s.Valid {
		return nil
	}
	return []byte(s.String)
}

// checkChain : chaque ligne est en v2, son empreinte et sa signature se
// recalculent à l'identique, et chaque parent est l'empreinte de la ligne
// précédente (GENESIS_HASH pour la première) : ni fourche, ni redémarrage.
func checkChain(t *testing.T, name string, rows []chainRow) {
	t.Helper()
	if len(rows) == 0 {
		t.Fatalf("%s: no row", name)
	}
	parents := map[string]int{}
	for i, r := range rows {
		if r.version != fiscal.HashVersion {
			t.Errorf("%s %s: hash_version %d, want %d", name, r.key, r.version, fiscal.HashVersion)
		}
		if r.recomputed != r.hash || r.recomputedSig != r.sig {
			t.Errorf("%s %s: recomputed hash/signature differ from stored ones", name, r.key)
		}
		want := fiscal.GenesisHash
		if i > 0 {
			want = rows[i-1].hash
		}
		if r.prev != want {
			t.Errorf("%s %s: previous hash %.12s…, want %.12s…", name, r.key, r.prev, want)
		}
		parents[r.prev]++
	}
	for p, n := range parents {
		if n > 1 {
			t.Errorf("%s: fork, parent %.12s… used %d times", name, p, n)
		}
	}
}

func (e *fiscalTestEnv) verifyReceiptNumbersContiguous(t *testing.T) {
	t.Helper()
	rows, err := e.db.QueryContext(context.Background(), `SELECT receipt_number FROM receipts WHERE merchant_id = $1`, e.merchantID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var seqs []int
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		seq, err := strconv.Atoi(n[strings.LastIndex(n, "-")+1:])
		if err != nil {
			t.Fatalf("receipt number %q: %v", n, err)
		}
		seqs = append(seqs, seq)
	}
	sort.Ints(seqs)
	for i, s := range seqs {
		if s != i+1 {
			t.Fatalf("receipt numbers not contiguous: %v", seqs)
		}
	}
}
