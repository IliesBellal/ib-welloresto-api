package stripeclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/client"
)

// Régression du bug corrigé dans ProcessPaymentIntentOnReader : l'appelant
// destructurait resolveOrCreatePaymentIntentLocked (qui retourne
// (clientSecret, paymentIntentID, err)) en `piID, _, resolveErr := ...`,
// récupérant donc le client_secret (format "pi_XXX_secret_YYY") au lieu de
// l'ID du PaymentIntent (format "pi_XXX"). Ce piID corrompu était ensuite
// envoyé tel quel à Stripe (`TerminalReaders.ProcessPaymentIntent`), qui le
// refusait avec "No such paymentintent: 'pi_XXX_secret_YYY'" — exactement
// l'erreur observée en logs serveur. Rien dans la suite existante n'appelait
// ProcessPaymentIntentOnReader avec un vrai/faux client Stripe : ce test
// ferme ce trou en vérifiant, au niveau HTTP, la valeur réellement transmise
// à Stripe.

// fakeTerminalAccountStore : un seul merchant/compte, jamais d'erreur —
// seule GetTerminalAccount est exercée par ProcessPaymentIntentOnReader.
type fakeTerminalAccountStore struct {
	accountID string
}

func (f *fakeTerminalAccountStore) GetTerminalAccount(_ context.Context, _ string) (string, error) {
	return f.accountID, nil
}

func (f *fakeTerminalAccountStore) GetTerminalLocationID(_ context.Context, _ string) (*string, error) {
	return nil, nil
}

// fakeTerminalPaymentStore : implémentation minimale en mémoire de
// TerminalPaymentStore. WithOrderLock n'a pas besoin d'un vrai verrou
// Postgres ici (un seul appel, une seule goroutine) — exécute simplement fn.
// Enregistre les paymentIntentID vus par SetKioskIDForPaymentIntent/
// MarkPaymentIntentStatus pour vérifier qu'eux aussi reçoivent l'ID, pas le
// client_secret (ils étaient corrompus par le même bug).
type fakeTerminalPaymentStore struct {
	kioskIDCallsWithPI []string
	statusCallsWithPI  []string
}

func (f *fakeTerminalPaymentStore) CreateMapping(_ context.Context, _, _ string) error {
	return nil
}

func (f *fakeTerminalPaymentStore) GetActivePaymentIntentForOrder(_ context.Context, _, _ string) (string, bool, error) {
	// Aucun PaymentIntent existant : force le chemin création
	// (resolveOrCreatePaymentIntentLocked → PaymentIntents.New), qui est
	// celui touché par le bug de destructuration.
	return "", false, nil
}

func (f *fakeTerminalPaymentStore) GetLatestPaymentIntentRecordForOrder(_ context.Context, _, _ string) (*PaymentIntentRecord, bool, error) {
	return nil, false, nil
}

func (f *fakeTerminalPaymentStore) GetMerchantIDForPaymentIntent(_ context.Context, _ string) (string, bool, error) {
	return "", false, nil
}

func (f *fakeTerminalPaymentStore) MarkPaymentIntentStatus(_ context.Context, paymentIntentID, _ string) error {
	f.statusCallsWithPI = append(f.statusCallsWithPI, paymentIntentID)
	return nil
}

func (f *fakeTerminalPaymentStore) SetKioskIDForPaymentIntent(_ context.Context, paymentIntentID, _ string) error {
	f.kioskIDCallsWithPI = append(f.kioskIDCallsWithPI, paymentIntentID)
	return nil
}

func (f *fakeTerminalPaymentStore) CountPaymentIntentAttemptsForOrder(_ context.Context, _, _ string) (int, error) {
	return 0, nil
}

func (f *fakeTerminalPaymentStore) WithOrderLock(ctx context.Context, _ string, fn func(txCtx context.Context) error) error {
	return fn(ctx)
}

// newTestStripeManager pointe un *client.API sur un httptest.Server local —
// même mécanisme que Backend/BackendConfig utilisé en production
// (StripeManager.Init), juste redirigé vers un serveur en mémoire plutôt que
// api.stripe.com.
func newTestStripeManager(serverURL string) *StripeManager {
	backends := stripe.NewBackendsWithConfig(&stripe.BackendConfig{
		URL: stripe.String(serverURL),
	})
	return &StripeManager{client: client.New("sk_test_dummy", backends)}
}

func TestProcessPaymentIntentOnReader_SendsRealPaymentIntentID_NotClientSecret(t *testing.T) {
	const (
		accountID  = "acct_test123"
		readerID   = "tmr_test123"
		merchantID = "merchant_test"
		orderID    = "order_test"
		kioskID    = "kiosk_test"
		idemKey    = "idem_test"
		newPIID    = "pi_3TestRegression000001"
	)
	// Format réel d'un client_secret Stripe : "{payment_intent_id}_secret_{random}".
	// C'est cette valeur (et non newPIID) qui partait par erreur vers Stripe
	// avant le correctif.
	newPIClientSecret := newPIID + "_secret_zzzRegressionzzz"

	var processPaymentIntentParam string
	processPaymentIntentCalls := 0

	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/payment_intents", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":%q,"object":"payment_intent","status":"requires_payment_method","client_secret":%q}`,
			newPIID, newPIClientSecret)
	})

	mux.HandleFunc("GET /v1/terminal/readers/"+readerID, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Aucune action en cours : dispatchToReader va directement au
		// process_payment_intent, sans passer par CancelReaderAction.
		fmt.Fprintf(w, `{"id":%q,"object":"terminal.reader","status":"online"}`, readerID)
	})

	mux.HandleFunc("POST /v1/terminal/readers/"+readerID+"/process_payment_intent", func(w http.ResponseWriter, r *http.Request) {
		processPaymentIntentCalls++
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse process_payment_intent form body: %v", err)
		}
		processPaymentIntentParam = r.FormValue("payment_intent")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":%q,"object":"terminal.reader","status":"online","action":{"type":"process_payment_intent","status":"in_progress"}}`, readerID)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected Stripe API call: %s %s", r.Method, r.URL.Path)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	payments := &fakeTerminalPaymentStore{}
	svc := NewTerminalService(
		newTestStripeManager(server.URL),
		&fakeTerminalAccountStore{accountID: accountID},
		payments,
	)

	gotPaymentIntentID, readerActionStatus, err := svc.ProcessPaymentIntentOnReader(
		context.Background(), merchantID, orderID, readerID, kioskID, idemKey,
		1500, 0.015, 10,
	)
	if err != nil {
		t.Fatalf("ProcessPaymentIntentOnReader returned an error: %v", err)
	}

	if processPaymentIntentCalls != 1 {
		t.Fatalf("process_payment_intent called %d times, want 1", processPaymentIntentCalls)
	}

	// L'assertion qui aurait détecté le bug avant même de le comprendre :
	// Stripe a reçu un vrai payment_intent_id, jamais un client_secret.
	if strings.Contains(processPaymentIntentParam, "_secret_") {
		t.Fatalf("payment_intent param sent to Stripe looks like a client_secret, not an ID: %q", processPaymentIntentParam)
	}
	if processPaymentIntentParam != newPIID {
		t.Fatalf("payment_intent param sent to Stripe = %q, want %q", processPaymentIntentParam, newPIID)
	}

	if gotPaymentIntentID != newPIID {
		t.Fatalf("ProcessPaymentIntentOnReader paymentIntentID = %q, want %q", gotPaymentIntentID, newPIID)
	}
	if readerActionStatus != string(stripe.TerminalReaderActionStatusInProgress) {
		t.Fatalf("readerActionStatus = %q, want %q", readerActionStatus, stripe.TerminalReaderActionStatusInProgress)
	}

	// Même vérification côté écritures DB (SetKioskIDForPaymentIntent /
	// MarkPaymentIntentStatus) : elles étaient, elles aussi, appelées avec le
	// client_secret avant le correctif — silencieusement no-op en base
	// (aucune ligne stripe_payments ne correspond à cette "clé").
	if len(payments.kioskIDCallsWithPI) != 1 || payments.kioskIDCallsWithPI[0] != newPIID {
		t.Fatalf("SetKioskIDForPaymentIntent calls = %v, want [%q]", payments.kioskIDCallsWithPI, newPIID)
	}
	if len(payments.statusCallsWithPI) != 1 || payments.statusCallsWithPI[0] != newPIID {
		t.Fatalf("MarkPaymentIntentStatus calls = %v, want [%q]", payments.statusCallsWithPI, newPIID)
	}
}
