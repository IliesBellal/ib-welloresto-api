package config

import "os"

type BrevoConfig struct {
	APIKey string
}

func loadBrevoConfig() BrevoConfig {
	return BrevoConfig{
		APIKey: os.Getenv("BREVO_API_KEY"),
	}
}
