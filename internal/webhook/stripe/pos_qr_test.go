package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	stripeclient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/models"
)

type fakePOSQRLifecycle struct {
	open      bool
	createErr error
	payments  []models.Payment
}

func (f *fakePOSQRLifecycle) OrderStillOpen(context.Context, string) (bool, error) {
	return f.open, nil
}

func (f *fakePOSQRLifecycle) CreatePayment(_ context.Context, p models.Payment) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.payments = append(f.payments, p)
	return nil
}

type fakePOSQRStripe struct {
	refunds []stripeclient.RefundRequest
}

func (f *fakePOSQRStripe) RefundOrCancelAsync(req stripeclient.RefundRequest) {
	f.refunds = append(f.refunds, req)
}

// Les frais ne sont pas relus en test : leur échec ne doit pas gêner le paiement.
func (f *fakePOSQRStripe) PaymentFees(string, string) (int64, int64, int64, error) {
	return 0, 0, 0, errors.New("no balance transaction yet")
}

const posQRSession = `{"id":"cs_1","amount_total":1250,"payment_status":"paid","payment_intent":"pi_1",
"customer_details":{"email":"client@example.com"},
"metadata":{"channel":"pos_qr","merchant_id":"212","order_id":"9001","cash_register_id":"77","user_id":"u5"}}`

func posQRService(lc *fakePOSQRLifecycle, st *fakePOSQRStripe) *StripeWebhookService {
	s := &StripeWebhookService{posQRLifecycle: lc}
	s.SetPOSQRStripe(st)
	return s
}

// La session payée crée un paiement STRIPE rattaché à la caisse et à
// l'utilisateur qui a affiché le QR code.
func TestPOSQR_CompletedRecordsPayment(t *testing.T) {
	lc, st := &fakePOSQRLifecycle{open: true}, &fakePOSQRStripe{}
	s := posQRService(lc, st)

	if err := s.dispatchEvent(context.Background(), withAccount(event("checkout.session.completed", json.RawMessage(posQRSession)), "acct_1")); err != nil {
		t.Fatal(err)
	}
	if len(lc.payments) != 1 {
		t.Fatalf("paiements = %d, want 1", len(lc.payments))
	}
	p := lc.payments[0]
	if p.MOP != models.StripeMOP || p.Amount != 1250 || p.OrderID != "9001" || p.MerchantID != "212" ||
		p.CashRegisterID != "77" || p.UserID != "u5" || p.OperationType != models.OperationTypeSale {
		t.Fatalf("paiement = %+v", p)
	}
	if p.PaymentIntentID == nil || *p.PaymentIntentID != "pi_1" || p.CheckoutSessionID == nil || *p.CheckoutSessionID != "cs_1" {
		t.Fatalf("références Stripe = %+v", p)
	}
	if p.CustomerEmail == nil || *p.CustomerEmail != "client@example.com" {
		t.Fatalf("e-mail = %v", p.CustomerEmail)
	}
	if len(st.refunds) != 0 {
		t.Fatalf("remboursement inattendu : %+v", st.refunds)
	}
}

// Session expirée : la commande n'est surtout pas refusée (le flux ScanNOrder
// le ferait), et aucun paiement n'est créé.
func TestPOSQR_ExpiredIsIgnored(t *testing.T) {
	lc := &fakePOSQRLifecycle{open: true}
	s := posQRService(lc, &fakePOSQRStripe{})
	// Sans dépendance du flux ScanNOrder (orderlifecycle nil) : un appel à
	// HandleCheckoutSessionCanceled paniquerait.
	if err := s.dispatchEvent(context.Background(), event("checkout.session.expired", json.RawMessage(posQRSession))); err != nil {
		t.Fatal(err)
	}
	if len(lc.payments) != 0 {
		t.Fatalf("paiements = %d, want 0", len(lc.payments))
	}
}

func TestPOSQR_NotPaidIsIgnored(t *testing.T) {
	lc := &fakePOSQRLifecycle{open: true}
	s := posQRService(lc, &fakePOSQRStripe{})
	unpaid := json.RawMessage(`{"id":"cs_1","amount_total":1250,"payment_status":"unpaid","metadata":{"channel":"pos_qr","merchant_id":"212","order_id":"9001"}}`)
	if err := s.dispatchEvent(context.Background(), event("checkout.session.completed", unpaid)); err != nil {
		t.Fatal(err)
	}
	if len(lc.payments) != 0 {
		t.Fatalf("paiements = %d, want 0", len(lc.payments))
	}
}

// Commande close ou déjà soldée : l'argent encaissé est rendu au client.
func TestPOSQR_RefundsWhenOrderCannotTakeIt(t *testing.T) {
	cases := map[string]*fakePOSQRLifecycle{
		"commande close":  {open: false},
		"commande soldée": {open: true, createErr: &models.OrderNotFullyPaidError{OrderID: "9001", PaidAmount: 2000, Price: 2000}},
	}
	for name, lc := range cases {
		st := &fakePOSQRStripe{}
		s := posQRService(lc, st)
		if err := s.dispatchEvent(context.Background(), withAccount(event("checkout.session.completed", json.RawMessage(posQRSession)), "acct_1")); err != nil {
			t.Fatalf("%s : %v", name, err)
		}
		if len(lc.payments) != 0 {
			t.Fatalf("%s : paiement enregistré", name)
		}
		if len(st.refunds) != 1 || st.refunds[0].IntentID != "pi_1" || st.refunds[0].AccountID != "acct_1" {
			t.Fatalf("%s : remboursements = %+v", name, st.refunds)
		}
	}
}

// Une erreur technique fait rejouer l'événement par Stripe, sans remboursement.
func TestPOSQR_TechnicalErrorIsRetried(t *testing.T) {
	lc, st := &fakePOSQRLifecycle{open: true, createErr: errors.New("db down")}, &fakePOSQRStripe{}
	s := posQRService(lc, st)
	if err := s.dispatchEvent(context.Background(), withAccount(event("checkout.session.completed", json.RawMessage(posQRSession)), "acct_1")); err == nil {
		t.Fatal("erreur attendue pour que Stripe rejoue l'événement")
	}
	if len(st.refunds) != 0 {
		t.Fatalf("remboursement inattendu : %+v", st.refunds)
	}
}

func withAccount(e StripeEvent, account string) StripeEvent {
	e.Account = account
	return e
}
