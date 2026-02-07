package stripeclient

import (
	"log/slog"

	"github.com/stripe/stripe-go/v84/client"
)

// StripeManager est l'implémentation concrète de notre pont.
type StripeManager struct {
	client *client.API  // Client officiel Stripe
	logger *slog.Logger // Utilisation du logger structuré de Go (slog)
}

// NewStripeManager crée une nouvelle instance du manager.
// apiKey : La clé secrète Stripe (sk_test_...)
// logger : Ton instance de log (pour tracer les erreurs)
func NewStripeManager(apiKey string, logger *slog.Logger) *StripeManager {
	sc := &client.API{}
	sc.Init(apiKey, nil)

	return &StripeManager{
		client: sc,
		logger: logger,
	}
}
