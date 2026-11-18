package stripe

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeAccountPayments struct {
	calls    int
	amount   int64
	paid     bool
	intent   string
	refunds  int
	refundPI string
	refunded int64
}

func (f *fakeAccountPayments) HandleOnlinePaymentCompleted(_ context.Context, _, paymentIntentID string, _ map[string]string, amount int64, paid bool) error {
	f.calls++
	f.amount, f.paid, f.intent = amount, paid, paymentIntentID
	return nil
}

func (f *fakeAccountPayments) HandleOnlinePaymentRefunded(_ context.Context, paymentIntentID string, totalRefunded int64) error {
	f.refunds++
	f.refundPI, f.refunded = paymentIntentID, totalRefunded
	return nil
}

// Paiement en ligne d'un compte client : la session payée est transmise au
// module des comptes clients ; ses autres événements sont ignorés (ils ne
// concernent aucune commande).
func event(typ string, obj json.RawMessage) StripeEvent {
	var e StripeEvent
	e.Type = typ
	e.Data.Object = obj
	return e
}

func TestDispatchEvent_CustomerAccount(t *testing.T) {
	fake := &fakeAccountPayments{}
	s := &StripeWebhookService{}
	s.SetCustomerAccountPayments(fake)

	session := json.RawMessage(`{"id":"cs_1","amount_total":2350,"payment_status":"paid","payment_intent":"pi_1","metadata":{"channel":"customer_account","merchant_id":"212","customer_id":"7"}}`)
	if err := s.dispatchEvent(context.Background(), event("checkout.session.completed", session)); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || fake.amount != 2350 || !fake.paid || fake.intent != "pi_1" {
		t.Fatalf("handler = %+v", fake)
	}

	// Remboursement (cumul remboursé de la charge) répercuté sur le compte.
	refund := json.RawMessage(`{"id":"ch_1","payment_intent":"pi_1","amount_refunded":1000,"metadata":{"channel":"customer_account"}}`)
	if err := s.dispatchEvent(context.Background(), event("charge.refunded", refund)); err != nil {
		t.Fatal(err)
	}
	if fake.refunds != 1 || fake.refundPI != "pi_1" || fake.refunded != 1000 {
		t.Fatalf("remboursement = %+v", fake)
	}

	charge := json.RawMessage(`{"id":"ch_1","metadata":{"channel":"customer_account"}}`)
	for _, typ := range []string{"charge.captured", "payment_intent.succeeded", "checkout.session.expired"} {
		if err := s.dispatchEvent(context.Background(), event(typ, charge)); err != nil {
			t.Fatalf("%s : %v", typ, err)
		}
	}
	if fake.calls != 1 {
		t.Fatalf("événements secondaires transmis : %d appels", fake.calls)
	}
}
