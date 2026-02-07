package config

import (
	"os"
)

type StripeConfig struct {
	APIKey string
}

func loadStripeConfig() StripeConfig {
	return StripeConfig{
		APIKey: os.Getenv("STRIPE_API_KEY"),
	}
}
