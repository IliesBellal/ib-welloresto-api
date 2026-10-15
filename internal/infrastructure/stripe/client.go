package stripeclient

import (
	"net/http"
	"time"

	requestlogger "welloresto-api/internal/middleware/request_logger"

	"github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/client"
)

// StripeManager est l'implémentation concrète de notre pont.
type StripeManager struct {
	client *client.API // Client officiel Stripe
}

// NewStripeManager crée une nouvelle instance du manager.
// apiKey : La clé secrète Stripe (sk_test_...)
// logger : Ton instance de log (pour tracer les erreurs)
func NewStripeManager(apiKey string) *StripeManager {
	sc := &client.API{}
	// Mêmes réglages que le client HTTP par défaut du SDK (80 s), avec en plus le
	// journal des appels sortants (api_request_logs). Chaque nouvelle tentative
	// réseau du SDK traverse le transport, donc est journalisée.
	backends := stripe.NewBackendsWithConfig(&stripe.BackendConfig{
		HTTPClient: &http.Client{Timeout: 80 * time.Second, Transport: requestlogger.Transport(nil)},
	})
	sc.Init(apiKey, backends)

	return &StripeManager{
		client: sc,
	}
}
