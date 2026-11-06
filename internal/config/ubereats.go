package config

import (
	"os"
	"strings"
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
		// Connexion d'un restaurant depuis le back-office (docs/marketplace-onboarding.md).
		// Les deux sont requises : sans elles, le bouton « Connecter » répond
		// « non configuré » plutôt que d'envoyer le restaurateur vers une
		// adresse de retour fausse.
		RedirectURI:      strings.TrimSpace(os.Getenv("UBER_EATS_REDIRECT_URI")),
		ConnectReturnURL: strings.TrimSpace(os.Getenv("UBER_EATS_CONNECT_RETURN_URL")),
	}
}
