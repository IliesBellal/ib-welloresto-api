//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	authpkg "welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/customers"
	"welloresto-api/internal/modules/notification"

	"go.uber.org/zap"
)

// TestComputeOrderTotals_POSOnly_Postgres vérifie que le recalcul serveur de
// TTC/HT/TVA (computeOrderTotals) ne s'applique qu'au flux POS
// (PrepareCreateOrder, seul appelant), pas aux commandes créées directement
// via CreateOrder — le chemin que partagent Kiosk, ScanNOrder et les
// webhooks Uber Eats/Deliveroo (cf. leur appel direct à
// OrdersLifeCycleService.CreateOrder, sans passer par Prepare*).
//
// Historique : le fix avait d'abord été placé dans
// OrdersLifeCycleRepository.CreateOrder/UpdateOrder elles-mêmes, donc
// appliqué à tous les canaux sans distinction — corrigé en le déplaçant dans
// PrepareCreateOrder/PrepareUpdateOrder, seul point d'entrée exclusif au POS
// (cf. docs/diagnostic-rapport-comptable-croq-o-pizzas.sql pour le contexte
// du bug d'origine).
//
// posRecomputeEnabled reflète l'état de la feature : false depuis le
// 2026-09-19 (appels commentés dans PrepareCreateOrder/PrepareUpdateOrder,
// cf. docs/decisions.md) — le POS conserve alors le TTC client comme les
// autres canaux. À passer à true en même temps que la réactivation.
const posRecomputeEnabled = false

func TestComputeOrderTotals_POSOnly_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM extra WHERE merchant_id = $1`,
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM device_link WHERE on_behalf_of IN (SELECT device_id FROM cash_registers WHERE merchant_id = $1)`,
			`DELETE FROM cash_registers WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_Id = $1`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-olc-scope' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest OLC Scope', 'a', '1', 's', '75001', 'Paris', 'siret-olc-scope', 'https://x', '06', 'mtok-olc-scope', 'UTC')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)
	t.Cleanup(func() { cleanupFor(merchantID) })

	if _, err := db.ExecContext(ctx, `INSERT INTO merchant_parameters (merchant_id, last_menu_update, cash_register_required_for_ordering) VALUES ($1, now(), true)`, merchantID); err != nil {
		t.Fatalf("seed params: %v", err)
	}

	// Produit à 1000 : tva_in_id/take_away/delivery valent 0 par défaut
	// ("TVA Undefined", taux 0 en donnée de référence réelle) -> HT = TTC,
	// seul TTC nous intéresse dans ce test.
	var prodID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO products (merchant_Id, name, price, category, status)
		VALUES ($1, 'itest-olc-scope-prod', 1000, 'c', '1') RETURNING product_id`, merchantID).Scan(&prodID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	prodIDStr := strconv.FormatInt(prodID, 10)

	// Caisse ouverte, nécessaire au flux POS (GetActiveCashRegisterID) —
	// Uber Eats/Deliveroo n'en ont pas besoin, ils fournissent leur propre
	// cash_register_id sans device_id (cf. plus bas).
	device := "itest-olc-scope-device"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO cash_registers (merchant_id, cash_desk_id, device_id, user_id, cash_fund, start_date, closure_comment)
		VALUES ($1, 1, $2, 'itest-olc-scope-user', 10000, now(), '')`, merchantID, device); err != nil {
		t.Fatalf("seed cash register: %v", err)
	}

	repo := NewOrdersLifeCycleRepository(db, customers.NewCustomerRepository(db))
	notifSvc := notification.NewNotificationService(notification.NewNotificationRepository(db), nil, nil, nil)
	svc := NewOrdersLifeCycleService(repo, nil, nil, nil, nil, zap.NewNop(), notifSvc, nil, nil, nil, nil, nil, db, nil, nil, nil, nil, nil)

	staffID := "999999"

	// --- POS (PrepareCreateOrder) : le TTC envoyé par le client (500, faux
	// vis-à-vis des lignes) doit être écrasé par le vrai total : 2 x 1000 =
	// 2000. ---
	posCtx := middleware.WithUser(ctx, &authpkg.UserLoginRow{UserID: staffID, MerchantID: merchantID})
	posReq := &models.RequestObject{
		DeviceID: &device,
		Order: models.OrderRequest{
			TTC: 500, TVA: 50, HT: 450, OrderType: "IN",
			Products: []models.OrderProductPayload{{ProductID: prodIDStr, Quantity: 2, Price: 1000}},
		},
	}
	posRes, err := svc.PrepareCreateOrder(posCtx, posReq)
	if err != nil || posRes.Status != "success" {
		t.Fatalf("PrepareCreateOrder(POS) = (%+v, %v)", posRes, err)
	}
	var posPrice int
	_ = db.QueryRowContext(ctx, `SELECT price FROM orders WHERE order_id = $1`, posRes.OrderID).Scan(&posPrice)
	wantPOSPrice := 500 // recalcul désactivé : le TTC client est conservé
	if posRecomputeEnabled {
		wantPOSPrice = 2000 // recalculé serveur : 2 x 1000
	}
	if posPrice != wantPOSPrice {
		t.Fatalf("POS: orders.price = %d, want %d (posRecomputeEnabled=%v)", posPrice, wantPOSPrice, posRecomputeEnabled)
	}

	// --- Uber Eats (CreateOrder direct, comme le fait le webhook) : le TTC
	// envoyé (500, tout aussi faux vis-à-vis des lignes) doit être conservé
	// tel quel — Uber Eats a déjà facturé ce montant au client final, notre
	// mapping TVA interne n'a pas vocation à s'y substituer. ---
	ueCreatedBy := models.UberEatsWebhookUserID
	ueReq := &models.RequestObject{
		MerchantID: merchantID,
		Order: models.OrderRequest{
			Brand: models.BrandUberEats, CreatedBy: &ueCreatedBy,
			CashRegisterId: strPtrOLC("external"),
			TTC:            500, TVA: 50, HT: 450, OrderType: "DELIVERY",
			Products: []models.OrderProductPayload{{ProductID: prodIDStr, Quantity: 2, Price: 1000}},
		},
	}
	ueRes, err := svc.CreateOrder(ctx, ueReq)
	if err != nil || ueRes.Status != "success" {
		t.Fatalf("CreateOrder(Uber Eats) = (%+v, %v)", ueRes, err)
	}
	var uePrice int
	_ = db.QueryRowContext(ctx, `SELECT price FROM orders WHERE order_id = $1`, ueRes.OrderID).Scan(&uePrice)
	if uePrice != 500 {
		t.Fatalf("Uber Eats: orders.price = %d, want 500 (le TTC de la plateforme, non recalculé)", uePrice)
	}

	// --- Deliveroo (même chemin direct) : même garantie, avec un TTC
	// différent pour ne pas confondre les deux cas par coïncidence. ---
	deliverooCreatedBy := "WEBHOOK_DELIVEROO"
	dlvReq := &models.RequestObject{
		MerchantID: merchantID,
		Order: models.OrderRequest{
			Brand: models.BrandDeliveroo, CreatedBy: &deliverooCreatedBy,
			CashRegisterId: strPtrOLC("external"),
			TTC:            700, TVA: 70, HT: 630, OrderType: "DELIVERY",
			Products: []models.OrderProductPayload{{ProductID: prodIDStr, Quantity: 2, Price: 1000}},
		},
	}
	dlvRes, err := svc.CreateOrder(ctx, dlvReq)
	if err != nil || dlvRes.Status != "success" {
		t.Fatalf("CreateOrder(Deliveroo) = (%+v, %v)", dlvRes, err)
	}
	var dlvPrice int
	_ = db.QueryRowContext(ctx, `SELECT price FROM orders WHERE order_id = $1`, dlvRes.OrderID).Scan(&dlvPrice)
	if dlvPrice != 700 {
		t.Fatalf("Deliveroo: orders.price = %d, want 700 (le TTC de la plateforme, non recalculé)", dlvPrice)
	}
}

func strPtrOLC(s string) *string { return &s }
