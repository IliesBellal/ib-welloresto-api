package config

import "os"

type GoogleConfig struct {
	APIKey string
	// ClientID is the OAuth 2.0 client id Google Sign-In id_tokens must carry
	// as their `aud` claim (LOT A Semaine 2, Chantier 7). Was optional at
	// startup through LOT A — unset ClientID did not block the API from
	// starting, POST /v1/auth/google simply errored at runtime without it
	// (same posture as ANTHROPIC_API_KEY/OPENAI_API_KEY, see CLAUDE.md).
	// LOT B B1 makes it fatal: googleauth.Verifier.Verify already fails
	// closed on an empty ClientID (returns an error before comparing `aud`
	// at all, never accepts a token), but a silently-broken Google Sign-In
	// in production is worse than a startup crash that gets noticed
	// immediately. Confirm the env var is set in every target environment
	// before deploying this change.
	ClientID string
}

func loadGoogle() GoogleConfig {
	return GoogleConfig{
		APIKey:   os.Getenv("GOOGLE_API_KEY"),
		ClientID: os.Getenv("GOOGLE_CLIENT_ID"),
	}
}
