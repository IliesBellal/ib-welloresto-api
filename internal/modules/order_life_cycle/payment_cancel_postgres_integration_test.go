//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/audit"
	authpkg "welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/modules/notification"

	"go.uber.org/zap"
)

// Lot C conformité caisse, phase 1 (docs/attestation-conformite-03-lot-C-brief.md) :
// les annulations de paiement de la caisse, de l'annulation et du refus de
// commande passent par fiscal.CancelPayments.
func TestPaymentCancellation_Service_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const siret = "siret-olc-paycxl"

	cleanupFor := func(mid string) {
		for _, q := range []string{
			`DELETE FROM audit_logs WHERE merchant_id = $1`,
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
		VALUES ('ITest OLC Pay Cancel', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mtok-olc-paycxl', 'UTC')
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
			VALUES ($1, 1, 'itest-olc-paycxl', 'itest', 0, now(), '', $2, CASE WHEN $2 THEN now() END) RETURNING cash_register_id`,
			merchantID, closed).Scan(&id); err != nil {
			t.Fatalf("seed register: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	orderNum := 0
	order := func(state string) string {
		t.Helper()
		orderNum++
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, merchant_approval, state, price, TVA, HT, isPaid, created_by)
			VALUES ($1, $2, 'WELLO_RESTO', 'PENDING', 'ACCEPTED', $3, 3000, 0, 3000, TRUE, 'itest') RETURNING order_id`,
			merchantID, orderNum, state).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	payment := func(orderID, mop string, amount int, registerID string) string {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, operation_type, enabled, payment_date, cash_register_id)
			VALUES ($1, 'itest', $2, $3, $4, 'SALE', TRUE, now(), $5) RETURNING payment_id`,
			merchantID, orderID, amount, mop, registerID).Scan(&id); err != nil {
			t.Fatalf("seed payment: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	enabled := func(paymentID string) bool {
		t.Helper()
		var e bool
		if err := db.QueryRowContext(ctx, `SELECT enabled FROM payments WHERE payment_id = $1`, paymentID).Scan(&e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	orderState := func(orderID string) (state, brandStatus string) {
		t.Helper()
		if err := db.QueryRowContext(ctx, `SELECT state, brand_status FROM orders WHERE order_id = $1`, orderID).Scan(&state, &brandStatus); err != nil {
			t.Fatal(err)
		}
		return
	}
	audits := func(action, resourceID string) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_logs WHERE merchant_id = $1 AND action = $2 AND resource_id = $3`,
			merchantID, action, resourceID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	custoRepo := customers.NewCustomerRepository(db)
	repo := NewOrdersLifeCycleRepository(db, custoRepo)
	auditSvc := audit.NewAuditService(audit.NewAuditRepository(db))
	notifSvc := notification.NewNotificationService(notification.NewNotificationRepository(db), nil, nil, nil)
	svc := NewOrdersLifeCycleService(repo, nil, nil, nil, nil, zap.NewNop(), notifSvc,
		customers.NewCustomersService(custoRepo, auditSvc), nil, auditSvc,
		&fakeOrderFetcher{resp: &models.PendingOrdersResponse{}}, nil, db, nil, nil, nil, nil, nil)
	staffCtx := middleware.WithUser(ctx, &authpkg.UserLoginRow{UserID: "999999", MerchantID: merchantID})

	openReg, closedReg := register(false), register(true)

	t.Run("caisse : annulation acceptée, tracée, commande non payée", func(t *testing.T) {
		o := order("OPEN")
		p := payment(o, "CB", 3000, openReg)
		if err := svc.DisablePayment(staffCtx, o, p); err != nil {
			t.Fatalf("DisablePayment: %v", err)
		}
		var isPaid bool
		_ = db.QueryRowContext(ctx, `SELECT isPaid FROM orders WHERE order_id = $1`, o).Scan(&isPaid)
		if enabled(p) || isPaid || audits(models.ActionPaymentCancelled, p) != 1 {
			t.Fatalf("enabled %v, isPaid %v, audit %d", enabled(p), isPaid, audits(models.ActionPaymentCancelled, p))
		}
	})

	t.Run("caisse : refus explicites (commande close, registre fermé, autre établissement)", func(t *testing.T) {
		closed := order("CLOSED")
		p1 := payment(closed, "CB", 3000, openReg)
		if err := svc.DisablePayment(staffCtx, closed, p1); !errors.Is(err, models.ErrPaymentOrderClosed) {
			t.Fatalf("closed order = %v, want ErrPaymentOrderClosed", err)
		}
		o := order("OPEN")
		p2 := payment(o, "ES", 3000, closedReg)
		if err := svc.DisablePayment(staffCtx, o, p2); !errors.Is(err, models.ErrPaymentRegisterClosed) {
			t.Fatalf("closed register = %v, want ErrPaymentRegisterClosed", err)
		}
		other := order("OPEN")
		p3 := payment(other, "CB", 3000, openReg)
		otherCtx := middleware.WithUser(ctx, &authpkg.UserLoginRow{UserID: "999999", MerchantID: "999999999"})
		if err := svc.DisablePayment(otherCtx, other, p3); !errors.Is(err, models.ErrNotFound) {
			t.Fatalf("other merchant = %v, want ErrNotFound", err)
		}
		if !enabled(p1) || !enabled(p2) || !enabled(p3) {
			t.Fatal("a refused cancellation changed a payment")
		}
	})

	t.Run("annulation de commande : paiement dans un registre fermé -> refus, rien ne change", func(t *testing.T) {
		o := order("OPEN")
		pOpen, pClosed := payment(o, "CB", 1000, openReg), payment(o, "ES", 2000, closedReg)
		err := svc.SetOrderDeleted(staffCtx, models.DenyOrderInput{OrderID: o, DeletionReasonID: "1", DeletionComment: "erreur"})
		if !errors.Is(err, models.ErrOrderPaymentRegisterClosed) {
			t.Fatalf("SetOrderDeleted = %v, want ErrOrderPaymentRegisterClosed", err)
		}
		if state, _ := orderState(o); state != "OPEN" || !enabled(pOpen) || !enabled(pClosed) || audits(models.ActionOrderDelete, o) != 0 {
			t.Fatalf("refusal changed something: state %s", state)
		}
	})

	t.Run("annulation de commande : paiements annulés avec leur trace", func(t *testing.T) {
		o := order("OPEN")
		p1, p2 := payment(o, "CB", 1000, openReg), payment(o, "ES", 2000, openReg)
		if err := svc.SetOrderDeleted(staffCtx, models.DenyOrderInput{OrderID: o, DeletionReasonID: "1", DeletionComment: "erreur"}); err != nil {
			t.Fatalf("SetOrderDeleted: %v", err)
		}
		state, status := orderState(o)
		if state != "CLOSED" || status != "CANCELED" || enabled(p1) || enabled(p2) {
			t.Fatalf("state %s, status %s, enabled %v %v", state, status, enabled(p1), enabled(p2))
		}
		if audits(models.ActionPaymentCancelled, p1) != 1 || audits(models.ActionPaymentCancelled, p2) != 1 || audits(models.ActionOrderDelete, o) != 1 {
			t.Fatal("missing audit entries")
		}
		var source string
		_ = db.QueryRowContext(ctx, `SELECT new_values->>'source' FROM audit_logs WHERE merchant_id = $1 AND resource_id = $2 AND action = $3`,
			merchantID, p1, models.ActionPaymentCancelled).Scan(&source)
		if source != fiscal.CancelSourceStaff {
			t.Fatalf("source = %q", source)
		}
	})

	t.Run("refus (tâche, Stripe) sur une commande close : refusé, commande et paiements intacts", func(t *testing.T) {
		o := order("CLOSED")
		p := payment(o, "STRIPE", 3000, "")
		err := svc.SetOrderDenied(ctx, o, models.DenyOrderRequest{
			MerchantID: merchantID, UserID: models.StripeWebhookUserID, DeletionReasonID: "43", DeletionComment: "Session expirée",
		})
		if !errors.Is(err, models.ErrOrderClosed) {
			t.Fatalf("SetOrderDenied(closed) = %v, want ErrOrderClosed", err)
		}
		if state, status := orderState(o); state != "CLOSED" || status != "PENDING" || !enabled(p) {
			t.Fatalf("closed order changed: %s %s", state, status)
		}
	})

	t.Run("refus d'une commande ouverte : paiement Stripe annulé, source STRIPE", func(t *testing.T) {
		o := order("OPEN")
		p := payment(o, "STRIPE", 3000, "")
		if err := svc.SetOrderDenied(ctx, o, models.DenyOrderRequest{
			MerchantID: merchantID, UserID: models.StripeWebhookUserID, DeletionReasonID: "43", DeletionComment: "Session expirée",
		}); err != nil {
			t.Fatalf("SetOrderDenied: %v", err)
		}
		if state, status := orderState(o); state != "CLOSED" || status != "DENIED" || enabled(p) {
			t.Fatalf("state %s, status %s, enabled %v", state, status, enabled(p))
		}
		var source string
		_ = db.QueryRowContext(ctx, `SELECT new_values->>'source' FROM audit_logs WHERE merchant_id = $1 AND resource_id = $2 AND action = $3`,
			merchantID, p, models.ActionPaymentCancelled).Scan(&source)
		if source != fiscal.CancelSourceStripe {
			t.Fatalf("source = %q", source)
		}
	})
}
