//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/audit"
	authpkg "welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/modules/notification"
	"welloresto-api/internal/modules/receipt"
	"welloresto-api/internal/utils/dbutils"

	"go.uber.org/zap"
)

// Lot C conformité caisse, phase 2 (docs/attestation-conformite-03-lot-C-brief.md) :
// une acceptation ne rouvre jamais une commande close ; la réouverture est
// contrôlée et tracée ; annuler une commande rouverte après une vente émet
// l'avoir de cette vente.
func TestReopenAndAccept_Service_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const siret = "siret-olc-reopen"

	cleanupFor := func(mid string) {
		for _, q := range []string{
			`DELETE FROM audit_logs WHERE merchant_id = $1`,
			`DELETE FROM receipts WHERE merchant_id = $1`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM cash_registers WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id::text = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = $1 LIMIT 1`, siret).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest OLC Reopen', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mtok-olc-reopen', 'UTC')
		RETURNING id`, siret).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)
	t.Cleanup(func() { cleanupFor(merchantID) })

	register := func(closed bool) string {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO cash_registers (merchant_id, cash_desk_id, device_id, user_id, cash_fund, start_date, closure_comment, closed, end_date)
			VALUES ($1, 1, 'itest-olc-reopen', 'itest', 0, now(), '', $2, CASE WHEN $2 THEN now() END) RETURNING cash_register_id`,
			merchantID, closed).Scan(&id); err != nil {
			t.Fatalf("seed register: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	orderNum := 0
	order := func(state, brandStatus string) string {
		t.Helper()
		orderNum++
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, merchant_approval, state, order_type, price, TVA, HT, delivered_on, created_by)
			VALUES ($1, $2, 'WELLO_RESTO', $3, 'PENDING_APPROVAL', $4::text, 'IN', 1000, 0, 1000, CASE WHEN $4::text = 'CLOSED' THEN now() END, 'itest')
			RETURNING order_id`, merchantID, orderNum, brandStatus, state).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	payment := func(orderID, registerID string) string {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, operation_type, enabled, payment_date, cash_register_id)
			VALUES ($1, 'itest', $2, 1000, 'CB', 'SALE', TRUE, now(), $3) RETURNING payment_id`,
			merchantID, orderID, registerID).Scan(&id); err != nil {
			t.Fatalf("seed payment: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	orderRow := func(orderID string) (state, brandStatus, approval string) {
		t.Helper()
		if err := db.QueryRowContext(ctx, `SELECT state, brand_status, merchant_approval FROM orders WHERE order_id = $1`, orderID).
			Scan(&state, &brandStatus, &approval); err != nil {
			t.Fatal(err)
		}
		return
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	custoRepo := customers.NewCustomerRepository(db)
	repo := NewOrdersLifeCycleRepository(db, custoRepo)
	auditSvc := audit.NewAuditService(audit.NewAuditRepository(db))
	receiptSvc := receipt.NewReceiptService(receipt.NewReceiptRepository(db))
	notifSvc := notification.NewNotificationService(notification.NewNotificationRepository(db), nil, nil, nil)
	svc := NewOrdersLifeCycleService(repo, nil, nil, nil, nil, zap.NewNop(), notifSvc,
		customers.NewCustomersService(custoRepo, auditSvc), nil, auditSvc,
		&fakeOrderFetcher{resp: &models.PendingOrdersResponse{}}, receiptSvc, db, nil, nil, nil, nil, nil)
	staffCtx := middleware.WithUser(ctx, &authpkg.UserLoginRow{UserID: "999999", MerchantID: merchantID})

	openReg, closedReg := register(false), register(true)

	t.Run("acceptation tardive sur une commande close : ignorée", func(t *testing.T) {
		o := order("CLOSED", "CLOSED")
		if _, err := svc.SetOrderAccepted(ctx, models.UberEatsWebhookUserID, merchantID, o); err != nil {
			t.Fatalf("SetOrderAccepted: %v", err)
		}
		if state, status, approval := orderRow(o); state != "CLOSED" || status != "CLOSED" || approval != "PENDING_APPROVAL" {
			t.Fatalf("closed order changed: %s %s %s", state, status, approval)
		}
		open := order("OPEN", "PENDING_APPROVAL")
		if _, err := svc.SetOrderAccepted(ctx, models.UberEatsWebhookUserID, merchantID, open); err != nil {
			t.Fatalf("SetOrderAccepted(open): %v", err)
		}
		if _, status, approval := orderRow(open); status != "PENDING" || approval != "ACCEPTED" {
			t.Fatalf("open order not accepted: %s %s", status, approval)
		}
	})

	t.Run("réouverture tracée puis annulation : avoir de la vente", func(t *testing.T) {
		o := order("CLOSED", "CLOSED")
		p := payment(o, openReg)
		ht := int64(1000)
		if err := dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
			return receiptSvc.GenerateFiscalReceipt(txCtx, &models.Order{OrderID: o, MerchantID: &merchantID, TTC: 1000, HT: &ht}, nil)
		}); err != nil {
			t.Fatal(err)
		}
		if err := svc.ReopenClosedOrder(staffCtx, o); err != nil {
			t.Fatalf("ReopenClosedOrder: %v", err)
		}
		if state, _, _ := orderRow(o); state != "OPEN" {
			t.Fatalf("state %s after reopen", state)
		}
		if count(`SELECT count(*) FROM audit_logs WHERE merchant_id = $1 AND action = $2 AND resource_id = $3`, merchantID, models.ActionOrderReopen, o) != 1 {
			t.Fatal("missing ORDER_REOPEN audit entry")
		}

		if err := svc.SetOrderDeleted(staffCtx, models.DenyOrderInput{OrderID: o, DeletionReasonID: "1", DeletionComment: "erreur"}); err != nil {
			t.Fatalf("SetOrderDeleted: %v", err)
		}
		if state, status, _ := orderRow(o); state != "CLOSED" || status != "CANCELED" {
			t.Fatalf("order %s %s after cancel", state, status)
		}
		if count(`SELECT count(*) FROM receipts WHERE order_id = $1 AND total_ttc = -1000 AND payments_snapshot = '[]'::jsonb`, o) != 1 ||
			count(`SELECT COALESCE(SUM(total_ttc), 0) FROM receipts WHERE order_id = $1`, o) != 0 {
			t.Fatal("expected one correction credit note cancelling the sale")
		}
		if count(`SELECT count(*) FROM payments WHERE payment_id = $1 AND enabled`, p) != 0 {
			t.Fatal("payment still enabled after cancellation")
		}
	})

	t.Run("réouverture refusée : paiement dans un registre fermé", func(t *testing.T) {
		o := order("CLOSED", "CLOSED")
		payment(o, closedReg)
		if err := svc.ReopenClosedOrder(staffCtx, o); !errors.Is(err, models.ErrReopenPaymentRegisterClosed) {
			t.Fatalf("ReopenClosedOrder = %v, want ErrReopenPaymentRegisterClosed", err)
		}
		if state, _, _ := orderRow(o); state != "CLOSED" {
			t.Fatalf("state %s after refused reopen", state)
		}
		if count(`SELECT count(*) FROM audit_logs WHERE merchant_id = $1 AND action = $2 AND resource_id = $3`, merchantID, models.ActionOrderReopen, o) != 0 {
			t.Fatal("refused reopen wrote an audit entry")
		}
	})
}
