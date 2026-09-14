package stripeclient

import (
	"github.com/stripe/stripe-go/v84"
)

// CreateCustomer creates a Stripe Customer on the platform account — the
// merchant, billed by WelloResto for its subscription (LOT B B2a). Distinct
// from every Connect-account customer this package's other files deal with:
// no StripeAccount header is set, so this always lands on the platform
// account itself, never a connected one.
func (s *StripeManager) CreateCustomer(name, email, merchantID string) (*stripe.Customer, error) {
	params := &stripe.CustomerParams{
		Name:  stripe.String(name),
		Email: stripe.String(email),
	}
	params.Metadata = map[string]string{"merchant_id": merchantID}
	return s.client.Customers.New(params)
}

// CreateSepaSetupIntent creates a SetupIntent restricted to SEPA Direct
// Debit, attached to customerID, for off-session future use (the recurring
// platform subscription charge — not something the merchant confirms each
// time). merchantID is stamped into metadata so the setup_intent.succeeded
// webhook can resolve which merchant this belongs to without a second
// lookup.
func (s *StripeManager) CreateSepaSetupIntent(customerID, merchantID string) (*stripe.SetupIntent, error) {
	params := &stripe.SetupIntentParams{
		Customer:           stripe.String(customerID),
		PaymentMethodTypes: []*string{stripe.String("sepa_debit")},
		Usage:              stripe.String("off_session"),
	}
	params.Metadata = map[string]string{"merchant_id": merchantID}
	return s.client.SetupIntents.New(params)
}

// GetPaymentMethod retrieves the full PaymentMethod object for id — needed
// because setup_intent.succeeded's webhook payload only carries
// payment_method as a bare ID reference (verified against the real Stripe
// test-mode API, LOT B B2b-0 : without an explicit expand, a SetupIntent's
// nested payment_method comes back with only .ID populated, .SEPADebit nil
// — every other field zero-valued). This is the only way to actually read
// sepa_debit.last4 for sepa_mandates.last4_iban_masked.
func (s *StripeManager) GetPaymentMethod(id string) (*stripe.PaymentMethod, error) {
	return s.client.PaymentMethods.Get(id, nil)
}

// RetryLatestOpenInvoice implements B2b-1's "réessayer maintenant" button —
// finds customerID's most recent open (unpaid) invoice and retries
// collection on it via the mandate/payment method already on file, no new
// IBAN entry (Invoices.Pay, off_session, using whatever default payment
// method the invoice/customer already has attached).
func (s *StripeManager) RetryLatestOpenInvoice(customerID string) error {
	params := &stripe.InvoiceListParams{
		Customer: stripe.String(customerID),
		Status:   stripe.String("open"),
	}
	params.Limit = stripe.Int64(1)
	iter := s.client.Invoices.List(params)
	if !iter.Next() {
		if err := iter.Err(); err != nil {
			return err
		}
		return nil // no open invoice — nothing to retry
	}
	invoice := iter.Invoice()
	_, err := s.client.Invoices.Pay(invoice.ID, &stripe.InvoicePayParams{})
	return err
}

// RecurringLineItem is the slice of subscriptions.StripeLineItem this
// package needs — kept as its own local type rather than importing
// internal/modules/subscriptions (an infra package importing a domain
// module would invert this repo's layering the wrong way); billing.Service
// maps between the two.
type RecurringLineItem struct {
	Code     string
	PriceID  string
	Quantity int64
}

// CreateSubscription creates the merchant's real recurring Stripe
// Subscription (LOT B B2c-0) — charge_automatically, billed against the
// payment method the just-succeeded SetupIntent attached (no separate
// confirmation step: the mandate IS the authorization). Each item's
// metadata carries its subscriptions_items code so
// SyncSubscriptionItems can later diff by code, not by Price ID alone.
func (s *StripeManager) CreateSubscription(customerID, paymentMethodID string, lines []RecurringLineItem, merchantID string) (*stripe.Subscription, error) {
	items := make([]*stripe.SubscriptionItemsParams, 0, len(lines))
	for _, l := range lines {
		item := &stripe.SubscriptionItemsParams{
			Price:    stripe.String(l.PriceID),
			Quantity: stripe.Int64(l.Quantity),
		}
		item.AddMetadata("code", l.Code)
		items = append(items, item)
	}
	params := &stripe.SubscriptionParams{
		Customer:             stripe.String(customerID),
		DefaultPaymentMethod: stripe.String(paymentMethodID),
		CollectionMethod:     stripe.String("charge_automatically"),
		Items:                items,
	}
	params.Metadata = map[string]string{"merchant_id": merchantID}
	return s.client.Subscriptions.New(params)
}

// SyncSubscriptionItems reconciles an EXISTING Stripe subscription's items
// with the desired lines, diffing by each existing item's metadata.code —
// never creates a second Subscription (B2c-0's explicit requirement).
// Existing items whose code isn't in lines are removed (Deleted: true);
// existing items whose code IS in lines get their quantity updated in
// place (by item ID, never re-created); codes with no existing item are
// added fresh.
func (s *StripeManager) SyncSubscriptionItems(subscriptionID string, lines []RecurringLineItem) error {
	existingByCode := map[string]*stripe.SubscriptionItem{}
	iter := s.client.SubscriptionItems.List(&stripe.SubscriptionItemListParams{Subscription: stripe.String(subscriptionID)})
	for iter.Next() {
		si := iter.SubscriptionItem()
		if code := si.Metadata["code"]; code != "" {
			existingByCode[code] = si
		}
	}
	if err := iter.Err(); err != nil {
		return err
	}

	wanted := map[string]RecurringLineItem{}
	for _, l := range lines {
		wanted[l.Code] = l
	}

	var items []*stripe.SubscriptionItemsParams
	for code, existing := range existingByCode {
		if l, ok := wanted[code]; ok {
			items = append(items, &stripe.SubscriptionItemsParams{
				ID:       stripe.String(existing.ID),
				Quantity: stripe.Int64(l.Quantity),
			})
		} else {
			items = append(items, &stripe.SubscriptionItemsParams{
				ID:      stripe.String(existing.ID),
				Deleted: stripe.Bool(true),
			})
		}
	}
	for code, l := range wanted {
		if _, ok := existingByCode[code]; !ok {
			item := &stripe.SubscriptionItemsParams{
				Price:    stripe.String(l.PriceID),
				Quantity: stripe.Int64(l.Quantity),
			}
			item.AddMetadata("code", code)
			items = append(items, item)
		}
	}

	if len(items) == 0 {
		return nil
	}
	_, err := s.client.Subscriptions.Update(subscriptionID, &stripe.SubscriptionParams{Items: items})
	return err
}

// HasAnyInvoice reports whether customerID has at least one Stripe invoice —
// used by B2a-1's attach-to endpoint to refuse retroactive merging onto a
// merchant that has already been billed on its own Customer.
func (s *StripeManager) HasAnyInvoice(customerID string) (bool, error) {
	params := &stripe.InvoiceListParams{Customer: stripe.String(customerID)}
	params.Limit = stripe.Int64(1)
	iter := s.client.Invoices.List(params)
	if iter.Next() {
		return true, nil
	}
	return false, iter.Err()
}
