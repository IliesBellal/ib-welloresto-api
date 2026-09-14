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
			_, _ = db.ExecContext(ctx, `DELETE FROM subscriptions WHERE merchant_id = $1`, merchantID)
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

	// --- Subscription (LOT B B2a-0): to_timestamp epoch handling, now
	// against subscriptions.current_period_end/status instead of the
	// retired subscription_invoices/CreateInvoice/PayInvoice path. ---
	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscriptions (merchant_id, package_id, stripe_subscription_id, billing_cycle, status)
		VALUES ($1, 1, '', 'monthly', 'setup')`, merchantID); err != nil {
		t.Fatalf("seed subscriptions: %v", err)
	}

	periodEnd := time.Now().UTC().Add(30 * 24 * time.Hour)
	if err := repo.UpdateSubscriptionBillingPeriod(ctx, merchantID, periodEnd.Unix()); err != nil {
		t.Fatalf("UpdateSubscriptionBillingPeriod failed against postgres: %v", err)
	}
	var gotPeriodEnd time.Time
	if err := db.QueryRowContext(ctx, `SELECT current_period_end FROM subscriptions WHERE merchant_id = $1`, merchantID).Scan(&gotPeriodEnd); err != nil {
		t.Fatalf("read back current_period_end: %v", err)
	}
	if diff := gotPeriodEnd.Sub(periodEnd); diff < -2*time.Second || diff > 2*time.Second {
		t.Fatalf("expected current_period_end ~= %v, got %v (diff %v) — check to_timestamp epoch handling", periodEnd, gotPeriodEnd, diff)
	}

	if err := repo.SetSubscriptionStatus(ctx, merchantID, "active"); err != nil {
		t.Fatalf("SetSubscriptionStatus failed against postgres: %v", err)
	}
	var gotStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM subscriptions WHERE merchant_id = $1`, merchantID).Scan(&gotStatus); err != nil {
		t.Fatalf("read back status: %v", err)
	}
	if gotStatus != "active" {
		t.Fatalf("expected status=active, got %q", gotStatus)
	}

	// --- GetMerchantIDByStripeSubscriptionID (LOT B F3): the fallback
	// resolveInvoiceMerchantID actually needs, found necessary by running a
	// real invoice.created webhook end-to-end and discovering
	// invoice.Metadata comes back empty — see docs/decisions.md. ---
	if _, err := db.ExecContext(ctx, `UPDATE subscriptions SET stripe_subscription_id = 'itest-sub-1' WHERE merchant_id = $1`, merchantID); err != nil {
		t.Fatalf("set stripe_subscription_id: %v", err)
	}
	if got, err := repo.GetMerchantIDByStripeSubscriptionID(ctx, "itest-sub-1"); err != nil {
		t.Fatalf("GetMerchantIDByStripeSubscriptionID failed against postgres: %v", err)
	} else if got != merchantID {
		t.Fatalf("GetMerchantIDByStripeSubscriptionID(%q) = %q, want %q", "itest-sub-1", got, merchantID)
	}
	if got, err := repo.GetMerchantIDByStripeSubscriptionID(ctx, "itest-sub-does-not-exist"); err != nil {
		t.Fatalf("GetMerchantIDByStripeSubscriptionID (no match) failed against postgres: %v", err)
	} else if got != "" {
		t.Fatalf("GetMerchantIDByStripeSubscriptionID (no match) = %q, want empty", got)
	}
	if got, err := repo.GetMerchantIDByStripeSubscriptionID(ctx, ""); err != nil {
		t.Fatalf("GetMerchantIDByStripeSubscriptionID (empty input) failed against postgres: %v", err)
	} else if got != "" {
		t.Fatalf("GetMerchantIDByStripeSubscriptionID (empty input) = %q, want empty", got)
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

// TestHandleInvoiceCreated_EmptyMetadata_ResolvesViaSubscriptionID_Postgres —
// LOT B F3: reproduces, at the handler level, exactly the shape of a real
// invoice.created webhook payload confirmed against the real Stripe API
// (metadata:{}, subscription:"sub_..."). Before this chantier's fix,
// HandleInvoiceCreated read only invoice.Metadata["merchant_id"], found it
// empty, and silently no-op'd — current_period_end was never written despite
// Stripe successfully delivering the webhook (200 OK). This test would have
// failed against the pre-fix code.
func TestHandleInvoiceCreated_EmptyMetadata_ResolvesViaSubscriptionID_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, logo_url, email)
		VALUES ('ITest F3 Invoice', 'addr', '1', 'street', '75001', 'Paris', 'siret-f3-invoice', 'https://example.com', '0600000000', 'tok-f3-invoice', 'Europe/Paris', 'https://example.com/logo.png', 'itest-f3-invoice@example.com')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(merchantIntID, 10)
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = db.ExecContext(bg, `DELETE FROM subscriptions WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(bg, `DELETE FROM merchant WHERE id = $1`, merchantIntID)
	})

	const stripeSubID = "sub_itest_f3_invoice"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscriptions (merchant_id, package_id, stripe_subscription_id, billing_cycle, status)
		VALUES ($1, 1, $2, 'monthly', 'active')`, merchantID, stripeSubID); err != nil {
		t.Fatalf("seed subscriptions: %v", err)
	}

	svc := &StripeWebhookService{repo: NewRepository(db)}

	periodEnd := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	// Mirrors the REAL raw webhook payload confirmed against the Stripe API
	// (LOT B F3): metadata is an empty object, subscription is a bare id
	// string (not expanded).
	payload := []byte(`{"id":"in_itest_f3","object":"invoice","customer":"cus_itest_f3","subscription":"` + stripeSubID + `","metadata":{},"period_end":` + strconv.FormatInt(periodEnd.Unix(), 10) + `}`)

	if err := svc.HandleInvoiceCreated(ctx, payload); err != nil {
		t.Fatalf("HandleInvoiceCreated: %v", err)
	}

	var gotPeriodEnd time.Time
	if err := db.QueryRowContext(ctx, `SELECT current_period_end FROM subscriptions WHERE merchant_id = $1`, merchantID).Scan(&gotPeriodEnd); err != nil {
		t.Fatalf("read back current_period_end: %v", err)
	}
	if diff := gotPeriodEnd.Sub(periodEnd); diff < -2*time.Second || diff > 2*time.Second {
		t.Fatalf("current_period_end not updated via subscription-id fallback: want ~%v, got %v", periodEnd, gotPeriodEnd)
	}
}
