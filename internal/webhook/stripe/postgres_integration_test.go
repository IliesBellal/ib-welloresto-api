//go:build postgres_integration

package stripe

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
)

func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func TestStripeRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	var orderIntID int64
	const accountID = "itest-acct-1"
	const stripeCustomerID = "itest-cus-1"
	const invoiceID = "itest-inv-1"

	cleanup := func() {
		if orderIntID != 0 {
			_, _ = db.ExecContext(ctx, `DELETE FROM orderitems WHERE order_id = $1`, orderIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM stripe_payments WHERE order_id = $1`, orderIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM payments WHERE order_id = $1`, orderIntID)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE order_id = $1`, orderIntID)
		}
		if merchantIntID != 0 {
			merchantID := strconv.FormatInt(merchantIntID, 10)
			_, _ = db.ExecContext(ctx, `DELETE FROM customer WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM subscription_invoices WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM welloresto_stripe_customers WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM stripe_accounts WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM scannorder_settings WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM qrcodes WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, merchantID)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
		}
	}
	t.Cleanup(func() { cleanup() })

	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, logo_url, email)
		VALUES ('ITest Stripe Merchant', 'addr', '1', 'street', '75001', 'Paris', 'siret-stripe', 'https://example.com', '0600000000', 'tok', 'Europe/Paris', 'https://example.com/logo.png', 'itest-merchant@example.com')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency, auto_accept_sno_delivery_orders, auto_accept_sno_take_away_orders)
		VALUES ($1, $2, 'EUR', true, false)`, merchantID, time.Now().UTC()); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO qrcodes (merchant_id, code) VALUES ($1, 'ITESTQR')`, merchantID); err != nil {
		t.Fatalf("seed qrcodes: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO stripe_accounts (account_id, merchant_id, verification_status)
		VALUES ($1, $2, 'action_required')`, accountID, merchantID); err != nil {
		t.Fatalf("seed stripe_accounts: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO welloresto_stripe_customers (merchant_id, stripe_customer_id) VALUES ($1, $2)`, merchantID, stripeCustomerID); err != nil {
		t.Fatalf("seed welloresto_stripe_customers: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO scannorder_settings (merchant_id, seo_title, seo_description, seo_keywords, seo_cuisine_type, activated)
		VALUES ($1, 't', 'd', 'k', 'french', false)`, merchantID); err != nil {
		t.Fatalf("seed scannorder_settings: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand_status, price, tva, ht, created_by, state, order_type)
		VALUES ($1, 1, 'PENDING_CARD_PAYMENT', 2000, 0, 2000, 'itest', 'OPEN', 'IN')
		RETURNING order_id`, merchantID).Scan(&orderIntID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	orderID := strconv.FormatInt(orderIntID, 10)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO orderitems (order_id, product_id, merchant_id, quantity, price)
		VALUES ($1, 9001, $2, 1, 2000)`, orderIntID, merchantID); err != nil {
		t.Fatalf("seed orderitem: %v", err)
	}

	repo := NewRepository(db)

	// GetMerchantByStripeAccountID: merchant.id/stripe_accounts.merchant_id CAST join fix.
	pm, err := repo.GetMerchantByStripeAccountID(ctx, accountID)
	if err != nil {
		t.Fatalf("GetMerchantByStripeAccountID failed against postgres: %v", err)
	}
	if pm == nil {
		t.Fatal("expected a payout merchant")
	}

	// GetMerchant: merchant/merchant_parameters/qrcodes CAST joins fix.
	merchant, err := repo.GetMerchant(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetMerchant failed against postgres: %v", err)
	}
	if merchant.Currency != "EUR" || merchant.Code != "ITESTQR" {
		t.Fatalf("unexpected merchant: %+v", merchant)
	}

	orderType, autoAccept, err := repo.GetAutoAcceptSettings(ctx, orderID, merchantID)
	if err != nil {
		t.Fatalf("GetAutoAcceptSettings failed against postgres: %v", err)
	}
	if orderType != "IN" || !autoAccept.AutoAcceptDelivery || autoAccept.AutoAcceptTakeaway {
		t.Fatalf("unexpected auto accept settings: type=%q %+v", orderType, autoAccept)
	}

	// InsertPayment: dbx.InsertReturningID + dbx.UTCNow().
	paymentID, err := repo.InsertPayment(ctx, Payment{MerchantID: merchantID, OrderID: orderID, Amount: 2000})
	if err != nil {
		t.Fatalf("InsertPayment failed against postgres: %v", err)
	}
	if paymentID == 0 {
		t.Fatal("expected a non-zero payment id")
	}

	// InsertStripePayment is dead code (never called from service.go /
	// http_handler.go) and its INSERT never sets stripe_payments.success_key
	// (NOT NULL, no default — confirmed identical in the MySQL source DDL),
	// so it fails on both dialects. Same pre-existing-bug class as
	// reservation.CreateBooking. Confirm the specific failure (so a
	// regression in the dbx.UTCNow() conversion would still be caught here),
	// then seed the row directly for the rest of this test.
	if err := repo.InsertStripePayment(ctx, StripePayment{
		OrderID: orderID, PaymentID: paymentID, PaymentIntentID: "itest-pi-1",
		CheckoutSessionID: "itest-cs-1", CustomerEmail: "itest@example.com",
	}); err == nil {
		t.Fatal("expected InsertStripePayment to fail on the pre-existing success_key NOT NULL bug")
	} else if !containsFold(err.Error(), "success_key") {
		t.Fatalf("expected a success_key NOT NULL violation, got: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO stripe_payments (order_id, payment_id, payment_intent_id, checkout_session_id, customer_email, success_key)
		VALUES ($1, $2, 'itest-pi-1', 'itest-cs-1', 'itest@example.com', 'itest-success-key-1')`,
		orderIntID, paymentID); err != nil {
		t.Fatalf("seed stripe_payments: %v", err)
	}

	// ConfirmKioskCardPayment first, while brand_status is still the seeded
	// PENDING_CARD_PAYMENT (UpdateOrderDetails below moves it to
	// PENDING_APPROVAL, which would make the guard a no-op).
	confirmed, err := repo.ConfirmKioskCardPayment(ctx, merchantID, orderID)
	if err != nil {
		t.Fatalf("ConfirmKioskCardPayment failed against postgres: %v", err)
	}
	if !confirmed {
		t.Fatal("expected ConfirmKioskCardPayment to report a change (order was PENDING_CARD_PAYMENT)")
	}

	// UpdateOrderPaymentStatus: UPDATE...JOIN -> correlated scalar subquery rewrite.
	if err := repo.UpdateOrderPaymentStatus(ctx, orderID); err != nil {
		t.Fatalf("UpdateOrderPaymentStatus failed against postgres: %v", err)
	}
	var isPaid bool
	if err := db.QueryRowContext(ctx, `SELECT ispaid FROM orders WHERE order_id = $1`, orderIntID).Scan(&isPaid); err != nil {
		t.Fatalf("read back isPaid: %v", err)
	}
	if !isPaid {
		t.Fatal("expected isPaid=true after UpdateOrderPaymentStatus (payment covers price)")
	}

	// UpdateOrderDetails: UPDATE...JOIN -> EXISTS rewrite.
	if err := repo.UpdateOrderDetails(ctx, "itest-cs-1", orderID); err != nil {
		t.Fatalf("UpdateOrderDetails failed against postgres: %v", err)
	}
	var brandStatus, approval string
	if err := db.QueryRowContext(ctx, `SELECT brand_status, merchant_approval FROM orders WHERE order_id = $1`, orderIntID).
		Scan(&brandStatus, &approval); err != nil {
		t.Fatalf("read back after UpdateOrderDetails: %v", err)
	}
	if brandStatus != "PENDING_APPROVAL" || approval != "PENDING_APPROVAL" {
		t.Fatalf("unexpected state after UpdateOrderDetails: status=%q approval=%q", brandStatus, approval)
	}

	// UpdateOrderItemsPaid: UPDATE...JOIN -> EXISTS rewrite.
	if err := repo.UpdateOrderItemsPaid(ctx, "itest-cs-1", orderID); err != nil {
		t.Fatalf("UpdateOrderItemsPaid failed against postgres: %v", err)
	}
	var itemPaid bool
	var paidQty int
	if err := db.QueryRowContext(ctx, `SELECT ispaid, paid_quantity FROM orderitems WHERE order_id = $1`, orderIntID).
		Scan(&itemPaid, &paidQty); err != nil {
		t.Fatalf("read back orderitem: %v", err)
	}
	if !itemPaid || paidQty != 1 {
		t.Fatalf("expected orderitem paid with paid_quantity=1, got paid=%v qty=%d", itemPaid, paidQty)
	}

	if err := repo.UpdateOrderCreationDate(ctx, orderID); err != nil {
		t.Fatalf("UpdateOrderCreationDate failed against postgres: %v", err)
	}

	order, err := repo.GetOrder(ctx, orderID)
	if err != nil {
		t.Fatalf("GetOrder failed against postgres: %v", err)
	}
	if order.Price != 2000 {
		t.Fatalf("unexpected order: %+v", order)
	}

	// --- Customers ---
	notFound, err := repo.FindCustomer(ctx, "nobody@example.com", merchantID)
	if err != nil {
		t.Fatalf("FindCustomer (empty) failed against postgres: %v", err)
	}
	if notFound != nil {
		t.Fatalf("expected nil, got %+v", notFound)
	}

	custID, err := repo.CreateCustomer(ctx, Customer{Name: "ITest Cust", Email: "itest-cust@example.com", Address: "addr"}, merchantID)
	if err != nil {
		t.Fatalf("CreateCustomer failed against postgres: %v", err)
	}
	if custID == 0 {
		t.Fatal("expected a non-zero customer id")
	}

	found, err := repo.FindCustomer(ctx, "itest-cust@example.com", merchantID)
	if err != nil {
		t.Fatalf("FindCustomer failed against postgres: %v", err)
	}
	if found == nil || found.ID != custID {
		t.Fatalf("unexpected found customer: %+v", found)
	}

	newAddr := "new addr"
	if err := repo.UpdateCustomer(ctx, Customer{ID: custID, Email: "itest-cust@example.com", Name: "ITest Cust Updated", Address: newAddr}); err != nil {
		t.Fatalf("UpdateCustomer failed against postgres: %v", err)
	}

	if err := repo.UpdateOrderCustomer(ctx, orderID, custID); err != nil {
		t.Fatalf("UpdateOrderCustomer failed against postgres: %v", err)
	}
	order, err = repo.GetOrder(ctx, orderID)
	if err != nil {
		t.Fatalf("GetOrder (after UpdateOrderCustomer) failed: %v", err)
	}
	if order.CustomerID == nil || *order.CustomerID != custID {
		t.Fatalf("expected order.CustomerID=%d, got %+v", custID, order.CustomerID)
	}

	// --- Fees & intents ---
	gotAccountID, err := repo.GetAccountIDByPaymentIntent(ctx, "itest-pi-1")
	if err != nil {
		t.Fatalf("GetAccountIDByPaymentIntent failed against postgres: %v", err)
	}
	if gotAccountID != accountID {
		t.Fatalf("expected %q, got %q", accountID, gotAccountID)
	}

	// UpdateFees: 2 UPDATE...JOIN rewrites (direct SET on stripe_payments, EXISTS on payments).
	if err := repo.UpdateFees(ctx, "itest-pi-1", 10, 50, 60); err != nil {
		t.Fatalf("UpdateFees failed against postgres: %v", err)
	}
	var fee, netAmount int64
	if err := db.QueryRowContext(ctx, `SELECT fee, net_amount FROM payments WHERE payment_id = $1`, paymentID).Scan(&fee, &netAmount); err != nil {
		t.Fatalf("read back payment fees: %v", err)
	}
	if fee != 60 || netAmount != 2000-60 {
		t.Fatalf("unexpected fee=%d net_amount=%d", fee, netAmount)
	}

	if err := repo.UpdatePaymentIntentStatus(ctx, "itest-pi-1", "succeeded"); err != nil {
		t.Fatalf("UpdatePaymentIntentStatus failed against postgres: %v", err)
	}

	// DisablePayment: UPDATE...JOIN -> EXISTS rewrite.
	if err := repo.DisablePayment(ctx, "itest-pi-1"); err != nil {
		t.Fatalf("DisablePayment failed against postgres: %v", err)
	}
	var paymentEnabled bool
	if err := db.QueryRowContext(ctx, `SELECT enabled FROM payments WHERE payment_id = $1`, paymentID).Scan(&paymentEnabled); err != nil {
		t.Fatalf("read back payment enabled: %v", err)
	}
	if paymentEnabled {
		t.Fatal("expected payment disabled after DisablePayment")
	}

	// --- Subscription: FROM_UNIXTIME -> to_timestamp fix ---
	created := time.Now().UTC().Add(-1 * time.Hour)
	if err := repo.CreateInvoice(ctx, merchantID, invoiceID, 1500, created.Unix(), stripeCustomerID); err != nil {
		t.Fatalf("CreateInvoice failed against postgres: %v", err)
	}
	var invoiceDate time.Time
	if err := db.QueryRowContext(ctx, `SELECT invoice_date FROM subscription_invoices WHERE invoice_id = $1`, invoiceID).Scan(&invoiceDate); err != nil {
		t.Fatalf("read back invoice_date: %v", err)
	}
	if diff := invoiceDate.Sub(created); diff < -2*time.Second || diff > 2*time.Second {
		t.Fatalf("expected invoice_date ~= %v, got %v (diff %v) — check to_timestamp epoch handling", created, invoiceDate, diff)
	}

	paidAt := time.Now().UTC()
	if err := repo.PayInvoice(ctx, invoiceID, paidAt.Unix()); err != nil {
		t.Fatalf("PayInvoice failed against postgres: %v", err)
	}
	var status int
	var paymentDate time.Time
	if err := db.QueryRowContext(ctx, `SELECT status, payment_date FROM subscription_invoices WHERE invoice_id = $1`, invoiceID).
		Scan(&status, &paymentDate); err != nil {
		t.Fatalf("read back after PayInvoice: %v", err)
	}
	if status != 1 {
		t.Fatalf("expected status=1, got %d", status)
	}
	if diff := paymentDate.Sub(paidAt); diff < -2*time.Second || diff > 2*time.Second {
		t.Fatalf("expected payment_date ~= %v, got %v (diff %v)", paidAt, paymentDate, diff)
	}

	// --- Connect account status ---
	if err := repo.UpdateStripeAccountVerificationStatus(ctx, accountID, "verified"); err != nil {
		t.Fatalf("UpdateStripeAccountVerificationStatus failed against postgres: %v", err)
	}
	gotMerchantID, err := repo.GetMerchantIDByStripeAccountID(ctx, accountID)
	if err != nil {
		t.Fatalf("GetMerchantIDByStripeAccountID failed against postgres: %v", err)
	}
	if gotMerchantID != merchantID {
		t.Fatalf("expected %q, got %q", merchantID, gotMerchantID)
	}

	if err := repo.SetScanNOrderActivated(ctx, merchantID, true); err != nil {
		t.Fatalf("SetScanNOrderActivated failed against postgres: %v", err)
	}
	var activated bool
	if err := db.QueryRowContext(ctx, `SELECT activated FROM scannorder_settings WHERE merchant_id = $1`, merchantID).Scan(&activated); err != nil {
		t.Fatalf("read back activated: %v", err)
	}
	if !activated {
		t.Fatal("expected scannorder activated=true")
	}
}
