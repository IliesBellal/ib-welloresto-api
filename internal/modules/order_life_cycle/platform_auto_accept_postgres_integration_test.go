//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/audit"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/modules/notification"
	"welloresto-api/internal/modules/receipt"

	"go.uber.org/zap"
)

// Acceptation automatique des commandes de plateformes
// (docs/acceptation-auto-commandes-plateformes.md) : refusée par la plateforme,
// une commande revient dans les commandes à accepter, sauf si elle a changé
// d'état entre-temps.
func TestPlatformAutoAccept_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const siret = "siret-olc-autoaccept"

	cleanupFor := func(mid string) {
		for _, q := range []string{
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
	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest OLC Platform Accept', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mtok-olc-autoaccept', 'UTC')
		RETURNING id`, siret).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)
	t.Cleanup(func() { cleanupFor(merchantID) })

	orderNum := 0
	order := func(brand, state, brandStatus, approval string) string {
		t.Helper()
		orderNum++
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, order_num, brand, brand_status, merchant_approval, state, order_type, price, TVA, HT, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, 'DELIVERY', 1000, 0, 1000, 'itest')
			RETURNING order_id`, merchantID, orderNum, brand, brandStatus, approval, state).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
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

	custoRepo := customers.NewCustomerRepository(db)
	repo := NewOrdersLifeCycleRepository(db, custoRepo)
	auditSvc := audit.NewAuditService(audit.NewAuditRepository(db))
	notifSvc := notification.NewNotificationService(notification.NewNotificationRepository(db), nil, nil, nil)
	svc := NewOrdersLifeCycleService(repo, nil, nil, nil, nil, zap.NewNop(), notifSvc,
		customers.NewCustomersService(custoRepo, auditSvc), nil, auditSvc,
		&fakeOrderFetcher{resp: &models.PendingOrdersResponse{}}, receipt.NewReceiptService(receipt.NewReceiptRepository(db)), db, nil, nil, nil, nil, nil)

	t.Run("auto-acceptation Deliveroo : commande acceptée", func(t *testing.T) {
		o := order(models.BrandDeliveroo, "OPEN", "PENDING_APPROVAL", "PENDING_APPROVAL")
		if err := svc.AutoAcceptOrder(ctx, models.DeliverooWebhookUserID, merchantID, o); err != nil {
			t.Fatalf("AutoAcceptOrder: %v", err)
		}
		if state, status, approval := orderRow(o); state != "OPEN" || status != "PENDING" || approval != "ACCEPTED" {
			t.Fatalf("order not accepted: %s %s %s", state, status, approval)
		}
	})

	t.Run("auto-acceptation d'une commande introuvable : erreur remontée", func(t *testing.T) {
		if err := svc.AutoAcceptOrder(ctx, models.UberEatsWebhookUserID, merchantID, "999999999"); err == nil {
			t.Fatal("AutoAcceptOrder on a missing order: want an error")
		}
	})

	t.Run("acceptation confirmée par Uber : rien n'est annulé", func(t *testing.T) {
		o := order(models.BrandUberEats, "OPEN", "ACCEPTED", "ACCEPTED")
		svc.finishPlatformAcceptance(merchantID, o, nil)
		if _, status, approval := orderRow(o); status != "ACCEPTED" || approval != "ACCEPTED" {
			t.Fatalf("accepted order changed: %s %s", status, approval)
		}
	})

	t.Run("acceptation refusée par Uber : retour dans les commandes à accepter", func(t *testing.T) {
		o := order(models.BrandUberEats, "OPEN", "PENDING_APPROVAL", "PENDING_APPROVAL")
		if _, err := repo.SetOrderAcceptedLocal(ctx, o); err != nil {
			t.Fatal(err)
		}
		svc.finishPlatformAcceptance(merchantID, o, errors.New("uber api error: 500"))
		if state, status, approval := orderRow(o); state != "OPEN" || status != "PENDING" || approval != "PENDING_APPROVAL" {
			t.Fatalf("order not back to approval: %s %s %s", state, status, approval)
		}
	})

	t.Run("refus après une annulation reçue entre-temps : annulation conservée", func(t *testing.T) {
		o := order(models.BrandUberEats, "CLOSED", "CANCELED", "ACCEPTED")
		svc.finishPlatformAcceptance(merchantID, o, errors.New("uber api error: 409"))
		if state, status, approval := orderRow(o); state != "CLOSED" || status != "CANCELED" || approval != "ACCEPTED" {
			t.Fatalf("canceled order changed: %s %s %s", state, status, approval)
		}
	})
}
