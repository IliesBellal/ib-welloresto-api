package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"welloresto-api/internal/models"
	stripeclient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/modules/subscriptions"

	"github.com/stripe/stripe-go/v84"
)

// stripeBillingClient is the slice of internal/infrastructure/stripe's
// StripeManager this package needs — a seam so tests can substitute a fake
// rather than requiring a live STRIPE_API_KEY (which this environment does
// not have) to exercise Service's own logic (which Customer to reuse vs.
// create, when to refuse a mutualisation). *stripeclient.StripeManager
// satisfies this interface as-is; routes.go passes it unchanged.
type stripeBillingClient interface {
	CreateCustomer(name, email, merchantID string) (*stripe.Customer, error)
	CreateSepaSetupIntent(customerID, merchantID string) (*stripe.SetupIntent, error)
	HasAnyInvoice(customerID string) (bool, error)
	// GetPaymentMethod — see billing.go's doc comment (B2b-0 finding): a
	// setup_intent.succeeded webhook's payment_method arrives as a bare ID
	// reference, never expanded, so this explicit follow-up call is the
	// only way HandleSetupIntentSucceeded can read sepa_debit.last4.
	GetPaymentMethod(id string) (*stripe.PaymentMethod, error)
	// CreateSubscription/SyncSubscriptionItems — LOT B B2c-0's real
	// recurring Stripe Subscription (see internal/infrastructure/stripe/billing.go).
	CreateSubscription(customerID, paymentMethodID string, lines []stripeclient.RecurringLineItem, merchantID string) (*stripe.Subscription, error)
	SyncSubscriptionItems(subscriptionID string, lines []stripeclient.RecurringLineItem) error
}

type Service struct {
	repo         *Repository
	stripe       stripeBillingClient
	subscriptions *subscriptions.Service
}

func NewService(repo *Repository, stripeMgr stripeBillingClient, subscriptionsSvc *subscriptions.Service) *Service {
	return &Service{repo: repo, stripe: stripeMgr, subscriptions: subscriptionsSvc}
}

// resolveOrCreateBillingCustomer implements B2a-2: reuse merchantID's
// existing platform_billing_customers row if it has one (including a
// shared/mutualized one — its stripe_customer_id is used as-is, never a
// second Customer created for an already-mutualized merchant), or create a
// brand new Stripe Customer + row otherwise.
func (s *Service) resolveOrCreateBillingCustomer(ctx context.Context, merchantID string) (string, error) {
	existing, err := s.repo.GetBillingCustomer(ctx, merchantID)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return existing.StripeCustomerID, nil
	}

	name, email, err := s.repo.GetMerchantOwnerContact(ctx, merchantID)
	if err != nil {
		return "", err
	}
	customer, err := s.stripe.CreateCustomer(name, email, merchantID)
	if err != nil {
		return "", err
	}
	if _, err := s.repo.UpsertBillingCustomer(ctx, merchantID, customer.ID, true); err != nil {
		return "", err
	}
	return customer.ID, nil
}

// CreateSepaSetup implements B2a-3's POST /v1/billing/sepa/setup: resolve or
// create the billing Customer (B2a-2), then a SetupIntent restricted to
// SEPA Direct Debit, and return its client_secret for the embedded Stripe
// Elements component (see docs/decisions.md for the Elements-vs-hosted-page
// choice this assumes).
func (s *Service) CreateSepaSetup(ctx context.Context, merchantID string) (clientSecret string, err error) {
	customerID, err := s.resolveOrCreateBillingCustomer(ctx, merchantID)
	if err != nil {
		return "", err
	}
	intent, err := s.stripe.CreateSepaSetupIntent(customerID, merchantID)
	if err != nil {
		return "", err
	}
	return intent.ClientSecret, nil
}

// GetActivationStatus — see Repository.GetActivationStatus's doc comment
// (B2b-3's bandeau, always fresh, never Redis-cached). Also folds in B2c-1's
// trial deadline, if this merchant currently has one.
func (s *Service) GetActivationStatus(ctx context.Context, merchantID string) (ActivationStatus, error) {
	st, err := s.repo.GetActivationStatus(ctx, merchantID)
	if err != nil {
		return ActivationStatus{}, err
	}
	trial, err := s.subscriptions.GetNearestActiveTrial(ctx, merchantID)
	if err != nil {
		return ActivationStatus{}, err
	}
	if trial != nil {
		st.TrialEndsAt = trial.TrialEndsAt
	}
	return st, nil
}

// AttachBillingCustomer implements B2a-1's
// POST /v1/admin/merchants/{id}/billing-customer/attach-to/{otherMerchantID} —
// makes merchantID share otherMerchantID's Stripe Customer (the
// mutualisation gesture, admin-only). Refuses if otherMerchantID has no
// billing customer of its own to share, and refuses if merchantID already
// has invoices on ITS OWN current Customer (no retroactive merging).
func (s *Service) AttachBillingCustomer(ctx context.Context, merchantID, otherMerchantID string) (BillingCustomer, error) {
	other, err := s.repo.GetBillingCustomer(ctx, otherMerchantID)
	if err != nil {
		return BillingCustomer{}, err
	}
	if other == nil {
		return BillingCustomer{}, models.ErrBillingCustomerNotFound
	}

	if existing, err := s.repo.GetBillingCustomer(ctx, merchantID); err != nil {
		return BillingCustomer{}, err
	} else if existing != nil {
		hasInvoices, err := s.stripe.HasAnyInvoice(existing.StripeCustomerID)
		if err != nil {
			return BillingCustomer{}, err
		}
		if hasInvoices {
			return BillingCustomer{}, models.ErrBillingCustomerHasInvoices
		}
	}

	return s.repo.UpsertBillingCustomer(ctx, merchantID, other.StripeCustomerID, false)
}

// DetachBillingCustomer implements B2a-1's
// POST /v1/admin/merchants/{id}/billing-customer/detach — creates a fresh
// Stripe Customer for merchantID and points it back at that instead of
// whatever it was sharing.
func (s *Service) DetachBillingCustomer(ctx context.Context, merchantID string) (BillingCustomer, error) {
	name, email, err := s.repo.GetMerchantOwnerContact(ctx, merchantID)
	if err != nil {
		return BillingCustomer{}, err
	}
	customer, err := s.stripe.CreateCustomer(name, email, merchantID)
	if err != nil {
		return BillingCustomer{}, err
	}
	return s.repo.UpsertBillingCustomer(ctx, merchantID, customer.ID, true)
}

// HandleSetupIntentSucceeded implements B2a-3's webhook completion: writes
// sepa_mandates, then activates the subscription and the merchant — always
// together (ActivateSubscriptionAndMerchant), and on nothing else. Decision
// N4b, re-verified here rather than assumed: this function never checks for
// a sellable product, a first direct-debit confirmation (that takes 5 days
// in SEPA), or any subscription_items content — the SetupIntent's own
// success is the entire condition.
//
// Takes raw JSON rather than a typed *stripe.SetupIntent because
// internal/webhook/stripe (the dispatcher) is still on stripe-go v78 while
// this package is on v84 (matching internal/infrastructure/stripe) — the
// wire format is identical either way, so unmarshalling here avoids forcing
// either side onto the other's SDK major version.
func (s *Service) HandleSetupIntentSucceeded(ctx context.Context, data json.RawMessage) error {
	var intent stripe.SetupIntent
	if err := json.Unmarshal(data, &intent); err != nil {
		return fmt.Errorf("unmarshal setup_intent: %w", err)
	}

	merchantID := intent.Metadata["merchant_id"]
	if merchantID == "" {
		return nil
	}

	var paymentMethodID string
	var last4 *string
	if intent.PaymentMethod != nil {
		paymentMethodID = intent.PaymentMethod.ID
		// B2b-0 (verified against the real Stripe test-mode API, see
		// docs/decisions.md): the webhook payload's payment_method is only
		// ever a bare ID reference (SEPADebit nil) — a plain field check
		// here would silently store last4=NULL forever. The follow-up call
		// is not an optimization, it is the only way to get this value at
		// all. If intent.PaymentMethod.SEPADebit were ever already
		// populated (a future Stripe change, or a test double), it's used
		// as-is without the extra call.
		if intent.PaymentMethod.SEPADebit != nil && intent.PaymentMethod.SEPADebit.Last4 != "" {
			l4 := intent.PaymentMethod.SEPADebit.Last4
			last4 = &l4
		} else if paymentMethodID != "" {
			full, err := s.stripe.GetPaymentMethod(paymentMethodID)
			if err != nil {
				return fmt.Errorf("fetch payment method %s: %w", paymentMethodID, err)
			}
			if full.SEPADebit != nil && full.SEPADebit.Last4 != "" {
				l4 := full.SEPADebit.Last4
				last4 = &l4
			}
		}
	}

	now := time.Now()
	if _, err := s.repo.CreateMandate(ctx, merchantID, paymentMethodID, MandateStatusActive, last4, &now); err != nil {
		return err
	}

	if err := s.repo.ActivateSubscriptionAndMerchant(ctx, merchantID); err != nil {
		return err
	}

	// LOT B B2c-0 : le mandat qui vient de réussir est aussi le moment de
	// créer (ou, si un item avait déjà été appliqué avant coup — cas rare —
	// de mettre à jour) l'abonnement Stripe récurrent réel. paymentMethodID
	// vient du SetupIntent qui vient de réussir : c'est l'autorisation elle-
	// même, aucune confirmation séparée n'est nécessaire.
	return s.CreateOrUpdateStripeSubscription(ctx, merchantID, paymentMethodID)
}

// SyncSubscriptionItems implements subscriptions.StripeSyncer — the hook
// ApplyItemChanges (B1e) calls after a real item change, so the merchant's
// Stripe subscription reflects the new composition. Never creates a second
// Subscription: if none exists yet for this merchant, this is a no-op by
// construction (CreateOrUpdateStripeSubscription only creates one when
// there's a payment method to attach it to, i.e. from
// HandleSetupIntentSucceeded — a merchant with no mandate yet has nothing
// for a real Stripe subscription to bill against regardless of what
// subscription_items says).
func (s *Service) SyncSubscriptionItems(ctx context.Context, merchantID string) error {
	existingSubID, err := s.repo.GetStripeSubscriptionID(ctx, merchantID)
	if err != nil {
		return err
	}
	if existingSubID == "" {
		return nil
	}
	return s.CreateOrUpdateStripeSubscription(ctx, merchantID, "")
}

// CreateOrUpdateStripeSubscription implements B2c-0 : résout les
// subscription_items actifs du marchand en vrais Price Stripe
// (subscriptions.Service.ResolveStripeLineItems), puis crée l'abonnement
// Stripe s'il n'en existe pas encore, ou met à jour l'existant sinon — ne
// crée jamais un second abonnement pour un marchand qui en a déjà un.
// paymentMethodID n'est utilisé que pour la création (une mise à jour
// réutilise le moyen de paiement déjà attaché à l'abonnement existant).
func (s *Service) CreateOrUpdateStripeSubscription(ctx context.Context, merchantID, paymentMethodID string) error {
	lines, err := s.subscriptions.ResolveStripeLineItems(ctx, merchantID)
	if err != nil {
		return err
	}
	stripeLines := make([]stripeclient.RecurringLineItem, 0, len(lines))
	for _, l := range lines {
		stripeLines = append(stripeLines, stripeclient.RecurringLineItem{Code: l.Code, PriceID: l.PriceID, Quantity: int64(l.Quantity)})
	}
	if len(stripeLines) == 0 {
		// Rien à facturer (ex. dérogation 'module' sans subscription_items
		// réel, voir B1d) — pas d'abonnement Stripe à créer/maintenir.
		return nil
	}

	existingSubID, err := s.repo.GetStripeSubscriptionID(ctx, merchantID)
	if err != nil {
		return err
	}
	if existingSubID != "" {
		return s.stripe.SyncSubscriptionItems(existingSubID, stripeLines)
	}

	customerID, err := s.resolveOrCreateBillingCustomer(ctx, merchantID)
	if err != nil {
		return err
	}
	sub, err := s.stripe.CreateSubscription(customerID, paymentMethodID, stripeLines, merchantID)
	if err != nil {
		return err
	}
	return s.repo.SetStripeSubscriptionID(ctx, merchantID, sub.ID)
}
