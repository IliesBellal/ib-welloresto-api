package config

import (
	"os"
	"welloresto-api/internal/modules/ubereats"
)

type UberEatsConfig = ubereats.ConfigUberEats

func loadUberEats() UberEatsConfig {
	return UberEatsConfig{
		BaseURL:      os.Getenv("UBER_EATS_BASE_URL"),
		ClientID:     os.Getenv("UBER_EATS_CLIENT_ID"),
		ClientSecret: os.Getenv("UBER_EATS_CLIENT_SECRET"),
		TokenType:    os.Getenv("UBER_EATS_TOKEN_TYPE"),
		AuthURL:      "https://auth.uber.com/oauth/v2/authorize",
		TokenURL:     "https://auth.uber.com/oauth/v2/token",
	}
}
