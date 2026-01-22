package config

import (
	"os"
	"welloresto-api/internal/modules/ubereats"
)

type UberEats = ubereats.ConfigUberEats

func loadUberEats() UberEats {
	return UberEats{
		BaseURL:      os.Getenv("UBER_EATS_BASE_URL"),
		ClientID:     os.Getenv("UBER_EATS_CLIENT_ID"),
		ClientSecret: os.Getenv("UBER_EATS_CLIENT_SECRET"),
		TokenType:    os.Getenv("UBER_EATS_TOKEN_TYPE"),
	}
}
