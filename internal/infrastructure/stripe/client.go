package stripeclient

import (
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
	sc.Init(apiKey, nil)

	return &StripeManager{
		client: sc,
	}
}
