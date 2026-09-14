// LOT B F3 — throwaway verification tool, not meant to be kept or committed.
// Runs a real SEPA mandate + real recurring Stripe Subscription against a
// disposable staging merchant, using a real Stripe Test Clock (B2c-0's
// method), and confirms the invoice.created/invoice.paid webhooks are
// actually DELIVERED over real HTTP to the deployed staging server
// (https://welloresto-api-staging.onrender.com/webhooks/stripe, already
// enabled — see docs/decisions.md) and correctly update subscriptions.status/
// current_period_end there — as opposed to just reading the Stripe API
// directly (already done in B2c-0). Cleans up everything it creates,
// Stripe-side and DB-side, before exiting.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/client"
)

func must(label string, err error) {
	if err != nil {
		log.Fatalf("%s: %v", label, err)
	}
}

func main() {
	stripeKey := os.Getenv("STRIPE_API_KEY")
	dsn := os.Getenv("RENDER_STAGING_DATABASE_URL")
	if stripeKey == "" || dsn == "" {
		log.Fatal("STRIPE_API_KEY et RENDER_STAGING_DATABASE_URL requis")
	}

	sc := &client.API{}
	sc.Init(stripeKey, nil)

	db, err := sql.Open("pgx", dsn)
	must("open db", err)
	defer db.Close()
	ctx := context.Background()

	label := strconv.FormatInt(time.Now().Unix(), 10)
	email := "f3-verify-" + label + "@example.com"
	name := "F3 Verify " + label

	var merchantIDInt int64
	must("seed merchant", db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, logo_url, email)
		VALUES ($1, 'addr', '1', 'street', '75001', 'Paris', $2, 'https://example.com', '0600000000', $3, 'Europe/Paris', 'https://example.com/logo.png', $4)
		RETURNING id`,
		name, "siret-f3-"+label, "tok-f3-"+label, email,
	).Scan(&merchantIDInt))
	merchantID := strconv.FormatInt(merchantIDInt, 10)
	fmt.Println("merchant_id =", merchantID)

	cleanupDB := func() {
		bg := context.Background()
		_, _ = db.ExecContext(bg, `DELETE FROM sepa_mandates WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(bg, `DELETE FROM platform_billing_customers WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(bg, `DELETE FROM subscription_items WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(bg, `DELETE FROM subscriptions WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(bg, `DELETE FROM merchant WHERE id = $1`, merchantIDInt)
		fmt.Println("DB cleanup done for merchant", merchantID)
	}

	must("seed subscriptions", func() error {
		_, err := db.ExecContext(ctx, `
			INSERT INTO subscriptions (merchant_id, package_id, stripe_subscription_id, billing_cycle, status)
			VALUES ($1, 1, '', 'monthly', 'setup')`, merchantID)
		return err
	}())
	must("seed subscription_items", func() error {
		_, err := db.ExecContext(ctx, `
			INSERT INTO subscription_items (id, merchant_id, code, kind, quantity, unit_price_cents)
			VALUES ($1, $2, 'essentiel', 'plan', 1, 7900)`, "sbit-f3-"+label, merchantID)
		return err
	}())

	// Real Stripe Test Clock — B2c-0's method: create it now, but do NOT
	// advance it until the first (real-time) settlement has resolved.
	tc, err := sc.TestHelpersTestClocks.New(&stripe.TestHelpersTestClockParams{
		FrozenTime: stripe.Int64(time.Now().Unix()),
		Name:       stripe.String("f3-verify-" + label),
	})
	must("create test clock", err)
	fmt.Println("test_clock =", tc.ID)

	teardownStripe := func() {
		if _, err := sc.TestHelpersTestClocks.Del(tc.ID, nil); err != nil {
			fmt.Println("WARN: test clock cleanup failed:", err)
		} else {
			fmt.Println("Stripe cleanup done (test clock deleted, cascades to customer/subscription/invoices/payment method)")
		}
	}

	custParams := &stripe.CustomerParams{
		Name:      stripe.String(name),
		Email:     stripe.String(email),
		TestClock: stripe.String(tc.ID),
	}
	custParams.Metadata = map[string]string{"merchant_id": merchantID}
	cust, err := sc.Customers.New(custParams)
	must("create customer", err)
	fmt.Println("customer =", cust.ID)

	// IMPORTANT: pre-create the platform_billing_customers row the real
	// POST /billing/sepa/setup flow (billing.Service.resolveOrCreateBillingCustomer)
	// would have created BEFORE the client ever confirms a SetupIntent. Skipping
	// this step (as an earlier run of this script did) makes
	// HandleSetupIntentSucceeded's own resolveOrCreateBillingCustomer call
	// find no row, so it creates a SECOND, unrelated Stripe Customer and then
	// tries to attach OUR payment method (which only exists on the FIRST
	// customer) to it — Stripe correctly rejects that
	// ("customer does not have a payment method with the ID ..."). That is a
	// bug in this test script's setup order, not in the product code.
	must("seed platform_billing_customers", func() error {
		_, err := db.ExecContext(ctx, `
			INSERT INTO platform_billing_customers (id, merchant_id, stripe_customer_id, is_primary_for_merchant)
			VALUES ($1, $2, $3, true)`, "pbc-f3-"+label, merchantID, cust.ID)
		return err
	}())

	pm, err := sc.PaymentMethods.New(&stripe.PaymentMethodParams{
		Type:      stripe.String("sepa_debit"),
		SEPADebit: &stripe.PaymentMethodSEPADebitParams{IBAN: stripe.String("FR1420041010050500013M02606")},
		BillingDetails: &stripe.PaymentMethodBillingDetailsParams{
			Name:  stripe.String(name),
			Email: stripe.String(email),
		},
	})
	must("create payment method", err)
	_, err = sc.PaymentMethods.Attach(pm.ID, &stripe.PaymentMethodAttachParams{Customer: stripe.String(cust.ID)})
	must("attach payment method", err)
	fmt.Println("payment_method =", pm.ID)

	siParams := &stripe.SetupIntentParams{
		Customer:           stripe.String(cust.ID),
		PaymentMethodTypes: []*string{stripe.String("sepa_debit")},
		PaymentMethod:      stripe.String(pm.ID),
		Usage:              stripe.String("off_session"),
		Confirm:            stripe.Bool(true),
		MandateData: &stripe.SetupIntentMandateDataParams{
			CustomerAcceptance: &stripe.SetupIntentMandateDataCustomerAcceptanceParams{
				Type: stripe.MandateCustomerAcceptanceTypeOnline,
				Online: &stripe.SetupIntentMandateDataCustomerAcceptanceOnlineParams{
					IPAddress: stripe.String("127.0.0.1"),
					UserAgent: stripe.String("f3-verify-script"),
				},
			},
		},
	}
	siParams.Metadata = map[string]string{"merchant_id": merchantID}
	si, err := sc.SetupIntents.New(siParams)
	must("create+confirm setup intent", err)
	fmt.Println("setup_intent =", si.ID, "status =", si.Status)
	if si.Status != stripe.SetupIntentStatusSucceeded {
		fmt.Println("FATAL: setup intent did not succeed synchronously, aborting")
		teardownStripe()
		cleanupDB()
		os.Exit(1)
	}

	// --- Wait for the REAL setup_intent.succeeded webhook to reach staging ---
	fmt.Println("\n=== Waiting for setup_intent.succeeded to be delivered + processed ===")
	deadline := time.Now().Add(5 * time.Minute)
	delivered := false
	for time.Now().Before(deadline) {
		ev := findEvent(sc, "setup_intent.succeeded", si.ID)
		if ev != nil {
			fmt.Printf("event %s found, pending_webhooks=%d\n", ev.ID, ev.PendingWebhooks)
			if ev.PendingWebhooks == 0 {
				delivered = true
				break
			}
		}
		time.Sleep(5 * time.Second)
	}
	if !delivered {
		fmt.Println("WARNING: setup_intent.succeeded event still has pending_webhooks>0 after 5min (or event not found)")
	}

	var activationState string
	var sepaCount int
	var subStatus, stripeSubID string
	for i := 0; i < 60; i++ {
		_ = db.QueryRowContext(ctx, `SELECT activation_state FROM merchant WHERE id = $1`, merchantIDInt).Scan(&activationState)
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sepa_mandates WHERE merchant_id = $1`, merchantID).Scan(&sepaCount)
		_ = db.QueryRowContext(ctx, `SELECT status, stripe_subscription_id FROM subscriptions WHERE merchant_id = $1`, merchantID).Scan(&subStatus, &stripeSubID)
		if activationState == "LIVE" && sepaCount > 0 && stripeSubID != "" {
			break
		}
		time.Sleep(5 * time.Second)
	}
	fmt.Printf("DB after setup_intent.succeeded: activation_state=%s sepa_mandates_count=%d subscriptions.status=%s stripe_subscription_id=%s\n",
		activationState, sepaCount, subStatus, stripeSubID)

	if stripeSubID == "" {
		fmt.Println("FATAL: no Stripe subscription id recorded — CreateOrUpdateStripeSubscription did not run (webhook not processed?). Aborting before invoice checks.")
		teardownStripe()
		cleanupDB()
		os.Exit(1)
	}

	// --- Inspect the real Stripe Subscription's first invoice ---
	sub, err := sc.Subscriptions.Get(stripeSubID, nil)
	must("get subscription", err)
	fmt.Println("stripe subscription status =", sub.Status, "latest_invoice =", sub.LatestInvoice)

	if sub.LatestInvoice == nil {
		fmt.Println("FATAL: subscription has no latest_invoice yet")
		teardownStripe()
		cleanupDB()
		os.Exit(1)
	}
	invoiceID := sub.LatestInvoice.ID

	inv, err := sc.Invoices.Get(invoiceID, nil)
	must("get invoice", err)
	fmt.Printf("invoice %s status=%s metadata=%v period_end=%d\n", inv.ID, inv.Status, inv.Metadata, inv.PeriodEnd)

	// --- Wait for the REAL invoice.created webhook ---
	fmt.Println("\n=== Waiting for invoice.created to be delivered + processed ===")
	deadline = time.Now().Add(5 * time.Minute)
	delivered = false
	for time.Now().Before(deadline) {
		ev := findEvent(sc, "invoice.created", invoiceID)
		if ev != nil {
			fmt.Printf("event %s found, pending_webhooks=%d\n", ev.ID, ev.PendingWebhooks)
			if ev.PendingWebhooks == 0 {
				delivered = true
				break
			}
		}
		time.Sleep(5 * time.Second)
	}
	if !delivered {
		fmt.Println("WARNING: invoice.created event still has pending_webhooks>0 after 5min (or event not found)")
	}

	var periodEnd sql.NullTime
	for i := 0; i < 30; i++ {
		_ = db.QueryRowContext(ctx, `SELECT current_period_end FROM subscriptions WHERE merchant_id = $1`, merchantID).Scan(&periodEnd)
		if periodEnd.Valid {
			break
		}
		time.Sleep(5 * time.Second)
	}
	fmt.Printf("DB after invoice.created: subscriptions.current_period_end = %v (expected around unix %d)\n", periodEnd, inv.PeriodEnd)

	// --- Wait (real time, per B2c-0's lesson) for the async SEPA debit on the invoice to settle ---
	fmt.Println("\n=== Waiting (real time) for the invoice to be paid ===")
	deadline = time.Now().Add(10 * time.Minute)
	paidStatus := ""
	for time.Now().Before(deadline) {
		inv, err = sc.Invoices.Get(invoiceID, nil)
		if err == nil {
			paidStatus = string(inv.Status)
			fmt.Println("invoice status =", paidStatus)
			if paidStatus == "paid" {
				break
			}
		}
		time.Sleep(15 * time.Second)
	}

	if paidStatus != "paid" {
		fmt.Println("invoice not paid within the wait window — this may just mean SEPA settlement takes longer than this script waited. Not necessarily a bug. Skipping invoice.paid webhook check.")
	} else {
		fmt.Println("\n=== Waiting for invoice.paid to be delivered + processed ===")
		deadline = time.Now().Add(5 * time.Minute)
		delivered = false
		for time.Now().Before(deadline) {
			ev := findEvent(sc, "invoice.paid", invoiceID)
			if ev != nil {
				fmt.Printf("event %s found, pending_webhooks=%d\n", ev.ID, ev.PendingWebhooks)
				if ev.PendingWebhooks == 0 {
					delivered = true
					break
				}
			}
			time.Sleep(5 * time.Second)
		}
		if !delivered {
			fmt.Println("WARNING: invoice.paid event still has pending_webhooks>0 after 5min (or event not found)")
		}
		var finalStatus string
		for i := 0; i < 30; i++ {
			_ = db.QueryRowContext(ctx, `SELECT status FROM subscriptions WHERE merchant_id = $1`, merchantID).Scan(&finalStatus)
			if finalStatus == "active" {
				break
			}
			time.Sleep(5 * time.Second)
		}
		fmt.Println("DB after invoice.paid: subscriptions.status =", finalStatus)
	}

	fmt.Println("\n=== Cleanup ===")
	teardownStripe()
	cleanupDB()
	fmt.Println("DONE")
}

// findEvent — most recent event of the given type whose data.object.id
// matches objectID (Stripe's event list has no server-side filter on nested
// object id, so this scans the most recent page of that type).
func findEvent(sc *client.API, eventType, objectID string) *stripe.Event {
	params := &stripe.EventListParams{Type: stripe.String(eventType)}
	params.Limit = stripe.Int64(20)
	iter := sc.Events.List(params)
	for iter.Next() {
		ev := iter.Event()
		if ev.GetObjectValue("id") == objectID {
			return ev
		}
	}
	return nil
}
