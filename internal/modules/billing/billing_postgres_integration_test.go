//go:build postgres_integration

package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	stripeclient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/pricing"
	"welloresto-api/internal/modules/subscriptions"

	"github.com/stripe/stripe-go/v84"
)

// newTestSubscriptionsService builds a real subscriptions.Service against
// the same db — harmless for this file's tests (none of them exercise
// ResolveStripeLineItems), just a required constructor argument now that
// billing.Service depends on it (B2c-0).
func newTestSubscriptionsService(db *sql.DB) *subscriptions.Service {
	pricingRepo := pricing.NewRepository(db)
	return subscriptions.NewService(db, subscriptions.NewRepository(db), pricingRepo, pricing.NewService(pricingRepo))
}

// fakeStripe is a test double for stripeBillingClient — this environment has
// no STRIPE_API_KEY, so Service's own logic (which Customer to reuse vs.
// create, when to refuse a mutualisation) is exercised against a real
// Postgres DB (staging, same convention as every other *_postgres_integration_test.go
// in this repo) with Stripe itself faked, rather than skipped outright.
type fakeStripe struct {
	nextCustomerN  int
	createdEmails  []string
	hasInvoice     map[string]bool
	setupIntentSeq int
	// paymentMethods lets tests simulate B2b-0's finding: a real
	// setup_intent.succeeded webhook only ever carries payment_method as a
	// bare {"id": "pm_..."} reference (SEPADebit nil) — the full object
	// (with sepa_debit.last4) is only available via a separate
	// GetPaymentMethod call, exactly like the real Stripe API.
	paymentMethods map[string]*stripe.PaymentMethod
	createSubCalls int
	nextSubN       int
	syncSubCalls   []string
}

func newFakeStripe() *fakeStripe {
	return &fakeStripe{hasInvoice: map[string]bool{}, paymentMethods: map[string]*stripe.PaymentMethod{}}
}

func (f *fakeStripe) GetPaymentMethod(id string) (*stripe.PaymentMethod, error) {
	pm, ok := f.paymentMethods[id]
	if !ok {
		return nil, fmt.Errorf("fakeStripe: no payment method registered for %s", id)
	}
	return pm, nil
}

func (f *fakeStripe) CreateCustomer(name, email, merchantID string) (*stripe.Customer, error) {
	f.nextCustomerN++
	f.createdEmails = append(f.createdEmails, email)
	return &stripe.Customer{ID: fmt.Sprintf("cus_fake_%d", f.nextCustomerN)}, nil
}

func (f *fakeStripe) CreateSepaSetupIntent(customerID, merchantID string) (*stripe.SetupIntent, error) {
	f.setupIntentSeq++
	return &stripe.SetupIntent{
		ID:           fmt.Sprintf("seti_fake_%d", f.setupIntentSeq),
		ClientSecret: fmt.Sprintf("seti_fake_%d_secret", f.setupIntentSeq),
		Customer:     &stripe.Customer{ID: customerID},
	}, nil
}

func (f *fakeStripe) HasAnyInvoice(customerID string) (bool, error) {
	return f.hasInvoice[customerID], nil
}

// CreateSubscription/SyncSubscriptionItems — B2c-0's stripeBillingClient
// additions. createCalls/syncCalls record what Service asked for, so tests
// can assert "never a second Subscription created" without a real Stripe
// call — the real-Stripe proof (test clocks, actual invoice.created) lives
// in docs/decisions.md as a manual verification run, not an automated test
// (no STRIPE_API_KEY in CI).
func (f *fakeStripe) CreateSubscription(customerID, paymentMethodID string, lines []stripeclient.RecurringLineItem, merchantID string) (*stripe.Subscription, error) {
	f.createSubCalls++
	f.nextSubN++
	return &stripe.Subscription{ID: fmt.Sprintf("sub_fake_%d", f.nextSubN)}, nil
}

func (f *fakeStripe) SyncSubscriptionItems(subscriptionID string, lines []stripeclient.RecurringLineItem) error {
	f.syncSubCalls = append(f.syncSubCalls, subscriptionID)
	return nil
}

// seedBillingMerchant inserts a minimal merchant row (activation_state
// defaults to 'SETUP', went_live_at NULL) and returns its text merchant_id.
func seedBillingMerchant(t *testing.T, db *sql.DB, label string) string {
	t.Helper()
	ctx := context.Background()
	var id int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, logo_url, email)
		VALUES ($1, 'addr', '1', 'street', '75001', 'Paris', $2, 'https://example.com', '0600000000', $3, 'Europe/Paris', 'https://example.com/logo.png', $4)
		RETURNING id`,
		"ITest Billing "+label, "siret-"+label, "t"+strconv.FormatInt(time.Now().UnixNano(), 36), "itest-billing-"+label+"@example.com",
	).Scan(&id); err != nil {
		t.Fatalf("seed merchant (%s): %v", label, err)
	}
	merchantID := strconv.FormatInt(id, 10)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM sepa_mandates WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM platform_billing_customers WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM subscriptions WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM merchant WHERE id = $1`, id)
	})
	return merchantID
}

// TestResolveOrCreateBillingCustomer_FirstSubscription_Postgres — B2a-2's
// lazy creation: no platform_billing_customers row yet -> a Stripe Customer
// is created and the row written with is_primary_for_merchant=true.
func TestResolveOrCreateBillingCustomer_FirstSubscription_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedBillingMerchant(t, db, "first-sub")

	fake := newFakeStripe()
	svc := NewService(NewRepository(db), fake, newTestSubscriptionsService(db))

	clientSecret, err := svc.CreateSepaSetup(ctx, merchantID)
	if err != nil {
		t.Fatalf("CreateSepaSetup: %v", err)
	}
	if clientSecret == "" {
		t.Fatal("clientSecret is empty")
	}
	if fake.nextCustomerN != 1 {
		t.Fatalf("expected exactly 1 Stripe Customer created, got %d", fake.nextCustomerN)
	}

	stored, err := svc.repo.GetBillingCustomer(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetBillingCustomer: %v", err)
	}
	if stored == nil || !stored.IsPrimaryForMerchant || stored.StripeCustomerID != "cus_fake_1" {
		t.Fatalf("GetBillingCustomer = %+v, want a primary row for cus_fake_1", stored)
	}
}

// TestResolveOrCreateBillingCustomer_SecondEstablishment_Postgres — the same
// operator's second establishment (a different merchant_id) gets its OWN
// distinct Stripe Customer by default — mutualisation never happens
// automatically, only via the admin attach-to endpoint (B2a-1).
func TestResolveOrCreateBillingCustomer_SecondEstablishment_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantA := seedBillingMerchant(t, db, "multi-a")
	merchantB := seedBillingMerchant(t, db, "multi-b")

	fake := newFakeStripe()
	svc := NewService(NewRepository(db), fake, newTestSubscriptionsService(db))

	if _, err := svc.CreateSepaSetup(ctx, merchantA); err != nil {
		t.Fatalf("CreateSepaSetup(A): %v", err)
	}
	if _, err := svc.CreateSepaSetup(ctx, merchantB); err != nil {
		t.Fatalf("CreateSepaSetup(B): %v", err)
	}
	if fake.nextCustomerN != 2 {
		t.Fatalf("expected 2 distinct Stripe Customers created, got %d", fake.nextCustomerN)
	}

	a, _ := svc.repo.GetBillingCustomer(ctx, merchantA)
	b, _ := svc.repo.GetBillingCustomer(ctx, merchantB)
	if a == nil || b == nil || a.StripeCustomerID == b.StripeCustomerID {
		t.Fatalf("expected distinct customers, got A=%+v B=%+v", a, b)
	}

	// Calling again for A must not create a third Customer — the existing
	// row is reused as-is.
	if _, err := svc.CreateSepaSetup(ctx, merchantA); err != nil {
		t.Fatalf("CreateSepaSetup(A) again: %v", err)
	}
	if fake.nextCustomerN != 2 {
		t.Fatalf("expected still 2 Stripe Customers after re-calling for A, got %d", fake.nextCustomerN)
	}
}

// TestAttachBillingCustomer_Mutualization_Postgres — B2a-1's admin-only
// mutualisation gesture.
func TestAttachBillingCustomer_Mutualization_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantA := seedBillingMerchant(t, db, "attach-a")
	merchantB := seedBillingMerchant(t, db, "attach-b")

	fake := newFakeStripe()
	svc := NewService(NewRepository(db), fake, newTestSubscriptionsService(db))

	if _, err := svc.CreateSepaSetup(ctx, merchantA); err != nil {
		t.Fatalf("CreateSepaSetup(A): %v", err)
	}
	aBefore, _ := svc.repo.GetBillingCustomer(ctx, merchantA)

	attached, err := svc.AttachBillingCustomer(ctx, merchantB, merchantA)
	if err != nil {
		t.Fatalf("AttachBillingCustomer: %v", err)
	}
	if attached.StripeCustomerID != aBefore.StripeCustomerID {
		t.Fatalf("attached.StripeCustomerID = %q, want A's %q", attached.StripeCustomerID, aBefore.StripeCustomerID)
	}
	if attached.IsPrimaryForMerchant {
		t.Fatal("attached.IsPrimaryForMerchant = true, want false (B shares A's Customer)")
	}

	// Refusal: the other merchant has no billing customer at all yet.
	merchantC := seedBillingMerchant(t, db, "attach-c-target")
	merchantD := seedBillingMerchant(t, db, "attach-d-empty-source")
	if _, err := svc.AttachBillingCustomer(ctx, merchantC, merchantD); !errors.Is(err, models.ErrBillingCustomerNotFound) {
		t.Fatalf("AttachBillingCustomer(no source) = %v, want ErrBillingCustomerNotFound", err)
	}

	// Refusal: the target already has invoices on its own Customer — no
	// retroactive merging.
	merchantE := seedBillingMerchant(t, db, "attach-e-has-invoices")
	if _, err := svc.CreateSepaSetup(ctx, merchantE); err != nil {
		t.Fatalf("CreateSepaSetup(E): %v", err)
	}
	eBefore, _ := svc.repo.GetBillingCustomer(ctx, merchantE)
	fake.hasInvoice[eBefore.StripeCustomerID] = true

	if _, err := svc.AttachBillingCustomer(ctx, merchantE, merchantA); !errors.Is(err, models.ErrBillingCustomerHasInvoices) {
		t.Fatalf("AttachBillingCustomer(target has invoices) = %v, want ErrBillingCustomerHasInvoices", err)
	}
	eAfter, _ := svc.repo.GetBillingCustomer(ctx, merchantE)
	if eAfter.StripeCustomerID != eBefore.StripeCustomerID {
		t.Fatal("merchantE's billing customer changed despite the refusal")
	}
}

// TestDetachBillingCustomer_Postgres — B2a-1's undo: a fresh, dedicated
// Stripe Customer replaces whatever was being shared.
func TestDetachBillingCustomer_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantA := seedBillingMerchant(t, db, "detach-a")
	merchantB := seedBillingMerchant(t, db, "detach-b")

	fake := newFakeStripe()
	svc := NewService(NewRepository(db), fake, newTestSubscriptionsService(db))

	if _, err := svc.CreateSepaSetup(ctx, merchantA); err != nil {
		t.Fatalf("CreateSepaSetup(A): %v", err)
	}
	if _, err := svc.AttachBillingCustomer(ctx, merchantB, merchantA); err != nil {
		t.Fatalf("AttachBillingCustomer: %v", err)
	}

	detached, err := svc.DetachBillingCustomer(ctx, merchantB)
	if err != nil {
		t.Fatalf("DetachBillingCustomer: %v", err)
	}
	if !detached.IsPrimaryForMerchant {
		t.Fatal("detached.IsPrimaryForMerchant = false, want true")
	}
	aNow, _ := svc.repo.GetBillingCustomer(ctx, merchantA)
	if detached.StripeCustomerID == aNow.StripeCustomerID {
		t.Fatal("merchantB still shares merchantA's Stripe customer after detach")
	}
}

// TestHandleSetupIntentSucceeded_ActivatesLive_Postgres — decision N4b: the
// merchant goes LIVE purely because the SetupIntent succeeded — no
// subscription_items row exists for this merchant at all here, and that is
// not checked.
func TestHandleSetupIntentSucceeded_ActivatesLive_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedBillingMerchant(t, db, "live")

	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscriptions (merchant_id, package_id, stripe_subscription_id, billing_cycle, status)
		VALUES ($1, 1, '', 'monthly', 'setup')`, merchantID); err != nil {
		t.Fatalf("seed subscriptions: %v", err)
	}

	var activationState string
	var wentLiveAt sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT activation_state, went_live_at FROM merchant WHERE id::text = $1`, merchantID).Scan(&activationState, &wentLiveAt); err != nil {
		t.Fatalf("read merchant before: %v", err)
	}
	if activationState == "LIVE" || wentLiveAt.Valid {
		t.Fatalf("merchant already LIVE before the mandate — test setup is wrong: %s / %+v", activationState, wentLiveAt)
	}

	fake := newFakeStripe()
	// B2b-0 (verified against the real Stripe test-mode API, see
	// docs/decisions.md): a real setup_intent.succeeded webhook's
	// payment_method is a bare ID reference only — no inline sepa_debit.
	// last4 is only resolvable via a follow-up GetPaymentMethod call, which
	// this registers against the fake exactly as the real API would answer.
	fake.paymentMethods["pm_itest_1"] = &stripe.PaymentMethod{
		ID:        "pm_itest_1",
		Type:      "sepa_debit",
		SEPADebit: &stripe.PaymentMethodSEPADebit{Last4: "1234"},
	}
	svc := NewService(NewRepository(db), fake, newTestSubscriptionsService(db))
	payload := []byte(fmt.Sprintf(`{
		"id": "seti_itest_1",
		"metadata": {"merchant_id": %q},
		"payment_method": "pm_itest_1"
	}`, merchantID))

	if err := svc.HandleSetupIntentSucceeded(ctx, payload); err != nil {
		t.Fatalf("HandleSetupIntentSucceeded: %v", err)
	}

	var mandate SepaMandate
	if err := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, stripe_payment_method_id, status, last4_iban_masked, accepted_at, created_at
		FROM sepa_mandates WHERE merchant_id = $1`, merchantID).
		Scan(&mandate.ID, &mandate.MerchantID, &mandate.StripePaymentMethodID, &mandate.Status, &mandate.Last4IBANMasked, &mandate.AcceptedAt, &mandate.CreatedAt); err != nil {
		t.Fatalf("read sepa_mandates: %v", err)
	}
	if mandate.StripePaymentMethodID != "pm_itest_1" || mandate.Status != MandateStatusActive || mandate.Last4IBANMasked == nil || *mandate.Last4IBANMasked != "1234" {
		t.Fatalf("sepa_mandates row = %+v, unexpected shape", mandate)
	}

	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM subscriptions WHERE merchant_id = $1`, merchantID).Scan(&status); err != nil {
		t.Fatalf("read subscriptions.status: %v", err)
	}
	if status != "active" {
		t.Fatalf("subscriptions.status = %q, want active", status)
	}

	var wentLiveAt2 time.Time
	if err := db.QueryRowContext(ctx, `SELECT activation_state, went_live_at FROM merchant WHERE id::text = $1`, merchantID).Scan(&activationState, &wentLiveAt2); err != nil {
		t.Fatalf("read merchant after: %v", err)
	}
	if activationState != "LIVE" {
		t.Fatalf("activation_state = %q, want LIVE — decision N4b (no other condition gates this)", activationState)
	}
	if wentLiveAt2.IsZero() {
		t.Fatal("went_live_at not set")
	}

	// Replay: a second setup_intent.succeeded for the same merchant must not
	// move went_live_at.
	if err := svc.HandleSetupIntentSucceeded(ctx, payload); err != nil {
		t.Fatalf("HandleSetupIntentSucceeded (replay): %v", err)
	}
	var wentLiveAt3 time.Time
	if err := db.QueryRowContext(ctx, `SELECT went_live_at FROM merchant WHERE id::text = $1`, merchantID).Scan(&wentLiveAt3); err != nil {
		t.Fatalf("read merchant after replay: %v", err)
	}
	if !wentLiveAt3.Equal(wentLiveAt2) {
		t.Fatalf("went_live_at changed on replay: %v -> %v", wentLiveAt2, wentLiveAt3)
	}
}

// TestCreateOrUpdateStripeSubscription_NeverDuplicates_Postgres — B2c-0's
// explicit requirement: a merchant who already has a
// subscriptions.stripe_subscription_id gets SyncSubscriptionItems, never a
// second CreateSubscription call, regardless of how many times this is
// invoked (e.g. a replayed setup_intent.succeeded, or several module
// changes in a row).
func TestCreateOrUpdateStripeSubscription_NeverDuplicates_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchantID := seedBillingMerchant(t, db, "no-dup-sub")

	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscriptions (merchant_id, package_id, stripe_subscription_id, billing_cycle, status)
		VALUES ($1, 1, '', 'monthly', 'active')`, merchantID); err != nil {
		t.Fatalf("seed subscriptions: %v", err)
	}
	subsRepo := subscriptions.NewRepository(db)
	if _, err := subsRepo.AddItem(ctx, merchantID, "essentiel", subscriptions.KindPlan, 1, 7900); err != nil {
		t.Fatalf("seed subscription_items: %v", err)
	}

	fake := newFakeStripe()
	svc := NewService(NewRepository(db), fake, newTestSubscriptionsService(db))

	if err := svc.CreateOrUpdateStripeSubscription(ctx, merchantID, "pm_fake_1"); err != nil {
		t.Fatalf("CreateOrUpdateStripeSubscription (1st, should CREATE): %v", err)
	}
	if fake.createSubCalls != 1 || len(fake.syncSubCalls) != 0 {
		t.Fatalf("after 1st call: createSubCalls=%d syncSubCalls=%v, want create=1 sync=0", fake.createSubCalls, fake.syncSubCalls)
	}
	subID, err := svc.repo.GetStripeSubscriptionID(ctx, merchantID)
	if err != nil || subID == "" {
		t.Fatalf("GetStripeSubscriptionID: %v / %q", err, subID)
	}

	// Called again (e.g. a module change, or a replayed webhook) — must
	// sync the EXISTING subscription, never create a second one.
	for i := 0; i < 2; i++ {
		if err := svc.CreateOrUpdateStripeSubscription(ctx, merchantID, "pm_fake_1"); err != nil {
			t.Fatalf("CreateOrUpdateStripeSubscription (repeat %d): %v", i, err)
		}
	}
	if fake.createSubCalls != 1 {
		t.Fatalf("createSubCalls = %d after repeated calls, want still 1 (no duplicate Subscription)", fake.createSubCalls)
	}
	if len(fake.syncSubCalls) != 2 || fake.syncSubCalls[0] != subID || fake.syncSubCalls[1] != subID {
		t.Fatalf("syncSubCalls = %v, want [%s %s]", fake.syncSubCalls, subID, subID)
	}
}
