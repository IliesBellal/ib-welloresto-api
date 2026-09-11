package config

import "os"

type GoogleConfig struct {
	APIKey string
	// ClientID is the OAuth 2.0 client id Google Sign-In id_tokens must carry
	// as their `aud` claim (LOT A Semaine 2, Chantier 7). Optional at
	// startup — unlike APIKey, unset ClientID does not block the API from
	// starting; POST /v1/auth/google simply errors at runtime without it
	// (same posture as ANTHROPIC_API_KEY/OPENAI_API_KEY, see CLAUDE.md).
	ClientID string
}

func loadGoogle() GoogleConfig {
	return GoogleConfig{
		APIKey:   os.Getenv("GOOGLE_API_KEY"),
		ClientID: os.Getenv("GOOGLE_CLIENT_ID"),
	}
}
