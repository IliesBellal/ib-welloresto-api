package config

import (
	"os"
)

type StripeConfig struct {
	APIKey string
	// OnboardingReturnURL is the front-end URL Stripe redirects to after onboarding completes.
	OnboardingReturnURL string
	// OnboardingRefreshURL is the front-end URL Stripe redirects to when the onboarding link expires.
	OnboardingRefreshURL string
}

func loadStripeConfig() StripeConfig {
	return StripeConfig{
		APIKey:               os.Getenv("STRIPE_API_KEY"),
		OnboardingReturnURL:  os.Getenv("STRIPE_ONBOARDING_RETURN_URL"),
		OnboardingRefreshURL: os.Getenv("STRIPE_ONBOARDING_REFRESH_URL"),
	}
}
