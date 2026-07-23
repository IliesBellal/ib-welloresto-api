//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers"
)

// Vérification réelle du module order_life_cycle contre le Postgres de dev :
// cycle complet commande/paiement (création, mise à jour, distribution,
// clôture avec chaînage fiscal), y compris l'effet de bord stripe_payments
// (rapport 20) et l'ex-procédure GET_AVERAGE_DISTRIBUTION_TIME (rapport 23,
// via distributiontime — cas « aucune donnée -> estimated_ready vide »).
func TestOrderLifeCycleRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		_, _ = db.ExecContext(ctx, `DELETE FROM device_link WHERE device_id LIKE 'itest-olc%'`)
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM restaurant_ticket WHERE merchant_id = $1`,
			`DELETE FROM stripe_payments WHERE order_id IN (SELECT order_id FROM orders WHERE merchant_id = $1)`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM order_comments WHERE order_id IN (SELECT order_id FROM orders WHERE merchant_id = $1)`,
			`DELETE FROM order_item_configuration WHERE order_item_id IN (SELECT order_item_id FROM orderitems WHERE merchant_id = $1)`,
			`DELETE FROM extra WHERE merchant_id = $1`,
			`DELETE FROM without WHERE merchant_id = $1`,
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM order_location WHERE order_id IN (SELECT order_id FROM orders WHERE merchant_id = $1)`,
			`DELETE FROM qrcodes WHERE merchant_id = $1`,
			`DELETE FROM bookings WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM customer WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_Id = $1`,
			`DELETE FROM locations WHERE merchant_id = $1`,
			`DELETE FROM cash_registers WHERE merchant_id = $1`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-olc' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	} else {
		cleanupFor("")
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest OLC', 'a', '1', 's', '75001', 'Paris', 'siret-olc', 'https://x', '06', 'mtok-olc', 'UTC')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)
	if _, err := db.ExecContext(ctx, `INSERT INTO merchant_parameters (merchant_id, last_menu_update, cash_register_required_for_ordering) VALUES ($1, now(), true)`, merchantID); err != nil {
		t.Fatalf("seed params: %v", err)
	}

	var prodA, prodBlocked int64
	if err := db.QueryRowContext(ctx, `INSERT INTO products (merchant_Id, name, price, category, status) VALUES ($1, 'itest-olc-ok', 1000, 'c', '1') RETURNING product_id`, merchantID).Scan(&prodA); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO products (merchant_Id, name, price, category, status) VALUES ($1, 'itest-olc-ko', 500, 'c', 'out_of_stock') RETURNING product_id`, merchantID).Scan(&prodBlocked); err != nil {
		t.Fatalf("seed blocked product: %v", err)
	}
	prodAStr := strconv.FormatInt(prodA, 10)

	var locID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO locations (merchant_id, location_name, seats) VALUES ($1, 'T-OLC', 2) RETURNING location_id`, merchantID).Scan(&locID); err != nil {
		t.Fatalf("seed location: %v", err)
	}

	// caisse ouverte liée à un device + device_link vers un second device
	var cashRegID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO cash_registers (merchant_id, cash_desk_id, device_id, user_id, cash_fund, start_date, closure_comment)
		VALUES ($1, 1, 'itest-olc-device', 'itest-olc-user', 10000, now(), '') RETURNING cash_register_id`, merchantID).Scan(&cashRegID); err != nil {
		t.Fatalf("seed cash register: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO device_link (device_id, on_behalf_of, user_id) VALUES ('itest-olc-linked', 'itest-olc-device', 'itest-olc-user')`); err != nil {
		t.Fatalf("seed device_link: %v", err)
	}

	repo := NewOrdersLifeCycleRepository(db, customers.NewCustomerRepository(db))

	// --- GetActiveCashRegisterID (direct + via device_link + absent) ---
	crStr := strconv.FormatInt(cashRegID, 10)
	if got, err := repo.GetActiveCashRegisterID(ctx, merchantID, "itest-olc-device"); err != nil || got != crStr {
		t.Fatalf("GetActiveCashRegisterID(direct) = (%q, %v), want %q", got, err, crStr)
	}
	if got, err := repo.GetActiveCashRegisterID(ctx, merchantID, "itest-olc-linked"); err != nil || got != crStr {
		t.Fatalf("GetActiveCashRegisterID(linked) = (%q, %v)", got, err)
	}
	if _, err := repo.GetActiveCashRegisterID(ctx, merchantID, "itest-olc-absent"); err != models.ErrNoCashRegisterOpen {
		t.Fatalf("GetActiveCashRegisterID(absent) = %v, want ErrNoCashRegisterOpen", err)
	}

	if required, err := repo.IsCashRegisterRequiredForOrdering(ctx, merchantID); err != nil || !required {
		t.Fatalf("IsCashRegisterRequiredForOrdering = (%v, %v)", required, err)
	}

	// --- CreateOrder : produit bloqué -> unavailable_products (IN dynamique) ---
	device := "itest-olc-device"
	createdBy := "itest-olc-user"
	custName := "Client OLC"
	custTel := "+33611223344"
	blockedReq := &models.RequestObject{
		MerchantID: merchantID, DeviceID: &device,
		Order: models.OrderRequest{
			TTC: 500, Products: []models.OrderProductPayload{{ProductID: strconv.FormatInt(prodBlocked, 10), Quantity: 1, Price: 500}},
			CreatedBy: &createdBy, OrderType: "IN",
		},
	}
	if res, err := repo.CreateOrder(ctx, blockedReq); err != nil || res.Status != "unavailable_products" {
		t.Fatalf("CreateOrder(bloqué) = (%+v, %v)", res, err)
	}

	// --- CreateOrder complet (client, items, extras/withouts/configs, table, paiement partiel) ---
	comment := "sans oignon svp"
	itemComment := "bien cuit"
	req := &models.RequestObject{
		MerchantID: merchantID, DeviceID: &device,
		Order: models.OrderRequest{
			TTC: 2000, TVA: 200, HT: 1800, OrderType: "IN", CreatedBy: &createdBy,
			Comment:  &comment,
			Customer: &models.CustomerRequest{Name: &custName, Tel: &custTel},
			Products: []models.OrderProductPayload{{
				ProductID: prodAStr, Quantity: 2, Price: 1000,
				Extra:   []*models.OrderExtraPayload{{ComponentID: "1", Price: 100}},
				Without: []*models.OrderWithoutPayload{{ComponentID: "2"}},
				Comment: &models.OrderItemCommentPayload{Content: itemComment},
			}},
			Locations: []models.OrderLocation{{LocationID: strconv.FormatInt(locID, 10)}},
			Payments:  []models.PaymentPayload{{Amount: 500, MOP: "ES"}},
		},
	}
	res, err := repo.CreateOrder(ctx, req)
	if err != nil || res.Status != "success" {
		t.Fatalf("CreateOrder = (%+v, %v)", res, err)
	}
	orderID := res.OrderID
	if *res.OrderNum != "1" {
		t.Fatalf("order_num = %q, want 1", *res.OrderNum)
	}
	var nItems, nExtras, nWithouts, nLocs, nComments, nPayments int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orderitems WHERE order_id = $1`, orderID).Scan(&nItems)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM extra WHERE order_id IN (SELECT order_item_id FROM orderitems WHERE order_id = $1)`, orderID).Scan(&nExtras)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM without WHERE merchant_id = $1`, merchantID).Scan(&nWithouts)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_location WHERE order_id = $1`, orderID).Scan(&nLocs)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_comments WHERE order_id = $1`, orderID).Scan(&nComments)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE order_id = $1`, orderID).Scan(&nPayments)
	if nItems != 1 || nExtras != 1 || nWithouts != 1 || nLocs != 1 || nComments != 2 || nPayments != 1 {
		t.Fatalf("compteurs après création = items %d, extras %d, withouts %d, locs %d, comments %d, payments %d", nItems, nExtras, nWithouts, nLocs, nComments, nPayments)
	}

	if num, err := repo.GetNextOrderNum(ctx, merchantID); err != nil || num != "2" {
		t.Fatalf("GetNextOrderNum = (%q, %v)", num, err)
	}
	if est, err := repo.ComputeEstimatedReady(ctx, merchantID, 2); err != nil || est != "" {
		t.Fatalf("ComputeEstimatedReady(sans données) = (%q, %v), want vide", est, err)
	}

	// --- AddPaymentAndReturnID : TR + stripe_payments + refresh isPaid ---
	code := "itest-olc-tr"
	trID, err := repo.AddPaymentAndReturnID(ctx, models.Payment{
		MerchantID: merchantID, CashRegisterID: crStr, OrderID: orderID,
		Amount: 500, MOP: models.TicketRestoMOP, UserID: createdBy,
		OperationType: models.OperationTypeSale, Code: &code,
	})
	if err != nil || trID == 0 {
		t.Fatalf("AddPaymentAndReturnID(TR) = (%d, %v)", trID, err)
	}
	var nTR int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM restaurant_ticket WHERE payment_id = $1`, trID).Scan(&nTR)
	if nTR != 1 {
		t.Fatalf("restaurant_ticket = %d", nTR)
	}

	intentID := "pi_itest_olc"
	stripePayID, err := repo.AddPaymentAndReturnID(ctx, models.Payment{
		MerchantID: merchantID, CashRegisterID: crStr, OrderID: orderID,
		Amount: 1000, MOP: "CB", UserID: createdBy,
		OperationType: models.OperationTypeSale, PaymentIntentID: &intentID,
	})
	if err != nil {
		t.Fatalf("AddPaymentAndReturnID(CB kiosk) : %v", err)
	}
	var nStripe int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stripe_payments WHERE payment_id = $1`, stripePayID).Scan(&nStripe)
	if nStripe != 1 {
		t.Fatalf("stripe_payments (effet de bord rapport 20) = %d", nStripe)
	}
	var isPaid bool
	_ = db.QueryRowContext(ctx, `SELECT isPaid FROM orders WHERE order_id = $1`, orderID).Scan(&isPaid)
	if !isPaid {
		t.Fatalf("isPaid devrait être TRUE après paiement complet (2000/2000)")
	}
	// sur-paiement -> erreur métier
	if _, err := repo.AddPaymentAndReturnID(ctx, models.Payment{
		MerchantID: merchantID, CashRegisterID: crStr, OrderID: orderID,
		Amount: 100, MOP: "ES", UserID: createdBy, OperationType: models.OperationTypeSale,
	}); err == nil {
		t.Fatalf("sur-paiement devrait échouer")
	}

	payments, err := repo.GetPaymentsForOrder(ctx, orderID)
	if err != nil || len(payments) != 3 {
		t.Fatalf("GetPaymentsForOrder = (%d, %v)", len(payments), err)
	}
	if p, err := repo.GetPayment(ctx, orderID, stripePayID); err != nil || p.IntentID == nil || *p.IntentID != intentID {
		t.Fatalf("GetPayment = (%+v, %v)", p, err)
	}

	// DisablePayment -> isPaid repasse à false (UPDATE ... FROM)
	if err := repo.DisablePayment(ctx, strconv.FormatInt(stripePayID, 10)); err != nil {
		t.Fatalf("DisablePayment: %v", err)
	}
	_ = db.QueryRowContext(ctx, `SELECT isPaid FROM orders WHERE order_id = $1`, orderID).Scan(&isPaid)
	if isPaid {
		t.Fatalf("isPaid devrait être FALSE après désactivation")
	}

	// --- UpdateOrder : maj item existant + nouvel item + retrait implicite ---
	var existingItemID string
	_ = db.QueryRowContext(ctx, `SELECT order_item_id FROM orderitems WHERE order_id = $1 LIMIT 1`, orderID).Scan(&existingItemID)
	oid := orderID
	req.Order.OrderID = &oid
	req.Order.TTC = 3000
	req.Order.Products = []models.OrderProductPayload{
		{ProductID: prodAStr, Quantity: 3, Price: 1000, OrderItemID: &existingItemID},
		{ProductID: prodAStr, Quantity: 1, Price: 1000},
	}
	req.Order.Payments = nil
	if err := repo.UpdateOrder(ctx, req); err != nil {
		t.Fatalf("UpdateOrder: %v", err)
	}
	var qty int
	_ = db.QueryRowContext(ctx, `SELECT quantity FROM orderitems WHERE order_item_id = $1`, existingItemID).Scan(&qty)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orderitems WHERE order_id = $1`, orderID).Scan(&nItems)
	if qty != 3 || nItems != 2 {
		t.Fatalf("UpdateOrder: qty=%d items=%d, want 3/2", qty, nItems)
	}

	// --- distribution / production ---
	if err := repo.SetDistributedProducts(ctx, createdBy, merchantID, &models.SetDistributedProductsRequest{
		OrderID: orderID, Products: []models.DistributedProduct{{OrderItemID: existingItemID, Quantity: 3}},
	}); err != nil {
		t.Fatalf("SetDistributedProducts: %v", err)
	}
	var brandStatus string
	_ = db.QueryRowContext(ctx, `SELECT brand_status FROM orders WHERE order_id = $1`, orderID).Scan(&brandStatus)
	if brandStatus != "PENDING" { // 1 item sur 2 -> pas totalement distribué
		t.Fatalf("brand_status après distribution partielle = %q", brandStatus)
	}
	if err := repo.MarkProductsBackToProduction(ctx, createdBy, merchantID, orderID, []models.DistributedProduct{{OrderItemID: existingItemID}}); err != nil {
		t.Fatalf("MarkProductsBackToProduction: %v", err)
	}
	if _, err := repo.UpdateProductionStatus(ctx, merchantID, &UpdateProductionStatusRequest{
		Products: []ProductionStatusProduct{{OrderItemID: existingItemID, OrderID: orderID, ProductionStatus: "DONE"}},
	}); err != nil {
		t.Fatalf("UpdateProductionStatus: %v", err)
	}
	if err := repo.SetReadyForDistribution(ctx, orderID, merchantID); err != nil {
		t.Fatalf("SetReadyForDistribution: %v", err)
	}

	// --- transitions ---
	if err := repo.SetOrderAcceptedLocal(ctx, orderID); err != nil {
		t.Fatalf("SetOrderAcceptedLocal: %v", err)
	}
	if open, err := repo.OrderStillOpen(ctx, orderID); err != nil || !open {
		t.Fatalf("OrderStillOpen = (%v, %v)", open, err)
	}
	// anomalie préexistante identique aux deux dialectes : le Scan de
	// MarkOrderAsDeliveryStarted lit brand_order_id (NULL pour une commande
	// WELLO_RESTO) dans un string non nullable — on pose une valeur pour
	// exercer le SQL converti.
	if _, err := db.ExecContext(ctx, `UPDATE orders SET brand_order_id = 'itest-bo' WHERE order_id = $1`, orderID); err != nil {
		t.Fatalf("set brand_order_id: %v", err)
	}
	if _, err := repo.MarkOrderAsDeliveryStarted(ctx, orderID, createdBy); err != nil {
		t.Fatalf("MarkOrderAsDeliveryStarted: %v", err)
	}
	if meta, err := repo.GetOrderBrandAndMerchant(ctx, orderID); err != nil || meta.MerchantID != merchantID {
		t.Fatalf("GetOrderBrandAndMerchant = (%+v, %v)", meta, err)
	}
	if brand, err := repo.GetOrderBrand(ctx, orderID); err != nil || brand != models.BrandWelloResto {
		t.Fatalf("GetOrderBrand = (%q, %v)", brand, err)
	}

	// --- clôture avec chaînage fiscal : re-payer intégralement puis livrer ---
	if _, err := repo.AddPaymentAndReturnID(ctx, models.Payment{
		MerchantID: merchantID, CashRegisterID: crStr, OrderID: orderID,
		Amount: 2000, MOP: "ES", UserID: createdBy, OperationType: models.OperationTypeSale,
	}); err != nil {
		t.Fatalf("re-paiement: %v", err)
	}
	meta, err := repo.SetDeliveredLocal(ctx, orderID)
	if err != nil || meta == nil {
		t.Fatalf("SetDeliveredLocal = (%+v, %v)", meta, err)
	}
	var state string
	var orderHash *string
	_ = db.QueryRowContext(ctx, `SELECT state, hash FROM orders WHERE order_id = $1`, orderID).Scan(&state, &orderHash)
	if state != "CLOSED" || orderHash == nil || *orderHash == "" {
		t.Fatalf("clôture = (%q, hash=%v)", state, orderHash)
	}
	if open, _ := repo.OrderStillOpen(ctx, orderID); open {
		t.Fatalf("OrderStillOpen après clôture")
	}
	if err := repo.ReopenClosedOrder(ctx, merchantID, orderID, createdBy); err != nil {
		t.Fatalf("ReopenClosedOrder: %v", err)
	}

	// --- annulation / divers ---
	if err := repo.DenyOrderLocal(ctx, orderID, "1", "test deny"); err != nil {
		t.Fatalf("DenyOrderLocal: %v", err)
	}
	if err := repo.DeleteOrderLocal(ctx, orderID, "1", "test delete"); err != nil {
		t.Fatalf("DeleteOrderLocal: %v", err)
	}
	if err := repo.DisablePayments(ctx, orderID); err != nil {
		t.Fatalf("DisablePayments: %v", err)
	}
	if err := repo.ClearBookings(ctx, orderID); err != nil {
		t.Fatalf("ClearBookings: %v", err)
	}
	if err := repo.DeleteQRCode(ctx, orderID); err != nil {
		t.Fatalf("DeleteQRCode: %v", err)
	}
	var custID string
	_ = db.QueryRowContext(ctx, `SELECT customer_id FROM customer WHERE merchant_id = $1 LIMIT 1`, merchantID).Scan(&custID)
	if err := repo.LinkCustomerToOrder(ctx, orderID, custID, merchantID); err != nil {
		t.Fatalf("LinkCustomerToOrder: %v", err)
	}

	// --- AddPaymentAndReturnID : upsert du mapping stripe_payments pré-créé
	// par un paiement Terminal (Kiosk), voir docs/KIOSK_DECISIONS.md, "Retrait
	// de Redis du mapping order_id/payment_intent_id" — ne doit PAS dupliquer
	// la ligne déjà présente pour le même payment_intent_id. Ordre isolé (pas
	// orderID ci-dessus, déjà fermé/re-payé à l'euro près) pour ne pas
	// perturber le garde de sur-paiement.
	kioskReq := &models.RequestObject{
		MerchantID: merchantID, DeviceID: &device,
		Order: models.OrderRequest{
			TTC: 1000, TVA: 100, HT: 900, OrderType: "IN", CreatedBy: &createdBy,
			Products: []models.OrderProductPayload{{ProductID: prodAStr, Quantity: 1, Price: 1000}},
		},
	}
	kioskRes, err := repo.CreateOrder(ctx, kioskReq)
	if err != nil || kioskRes.Status != "success" {
		t.Fatalf("CreateOrder(kiosk) = (%+v, %v)", kioskRes, err)
	}
	kioskOrderID := kioskRes.OrderID

	kioskIntentID := "pi_itest_olc_kiosk_precreated"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO stripe_payments (order_id, payment_intent_id, success_key)
		VALUES ($1, $2, '')`, kioskOrderID, kioskIntentID); err != nil {
		t.Fatalf("seed pre-created stripe_payments: %v", err)
	}

	kioskPayID, err := repo.AddPaymentAndReturnID(ctx, models.Payment{
		MerchantID: merchantID, OrderID: kioskOrderID,
		Amount: 1000, MOP: models.CardMOP, UserID: "KIOSK",
		OperationType: models.OperationTypeSale, PaymentIntentID: &kioskIntentID,
	})
	if err != nil {
		t.Fatalf("AddPaymentAndReturnID(kiosk pre-created mapping): %v", err)
	}

	var nMappingRows int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stripe_payments WHERE payment_intent_id = $1`, kioskIntentID).Scan(&nMappingRows); err != nil {
		t.Fatalf("count stripe_payments for kiosk intent: %v", err)
	}
	if nMappingRows != 1 {
		t.Fatalf("stripe_payments rows for pre-created kiosk mapping = %d, want 1 (upsert must not duplicate)", nMappingRows)
	}
	var completedPaymentID int64
	if err := db.QueryRowContext(ctx, `SELECT payment_id FROM stripe_payments WHERE payment_intent_id = $1`, kioskIntentID).Scan(&completedPaymentID); err != nil {
		t.Fatalf("read back completed stripe_payments row: %v", err)
	}
	if completedPaymentID != kioskPayID {
		t.Fatalf("stripe_payments.payment_id = %d, want %d (the pre-created row must be completed in place, not duplicated)", completedPaymentID, kioskPayID)
	}
}
