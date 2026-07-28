package config

import "os"

type BrevoConfig struct {
	APIKey       string
	WebhookToken string
}

func loadBrevoConfig() BrevoConfig {
	return BrevoConfig{
		APIKey:       os.Getenv("BREVO_API_KEY"),
		WebhookToken: os.Getenv("BREVO_WEBHOOK_TOKEN"),
	}
}
