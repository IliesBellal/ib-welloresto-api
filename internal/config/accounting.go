package config

import (
	"os"
	"strings"
)

// AccountingEncryptionKeyEnvVar : clé AES-256 (base64, 32 octets) qui chiffre
// les identifiants des logiciels de comptabilité (`openssl rand -base64 32`).
// Distincte de KIOSK_PIN_ENCRYPTION_KEY. Sans elle, aucune connexion n'est
// possible (jamais de repli sur une autre clé).
const AccountingEncryptionKeyEnvVar = "ACCOUNTING_ENCRYPTION_KEY"

// AccountingConfig règle la synchronisation comptable (docs/accounting-sync.md).
type AccountingConfig struct {
	// ReturnURL : page du back-office où l'on revient après l'autorisation OAuth
	// d'un outil (ACCOUNTING_RETURN_URL).
	ReturnURL string
	Pennylane PennylaneConfig
}

// PennylaneConfig : application OAuth de l'éditeur chez Pennylane. Tant que
// PENNYLANE_CLIENT_ID / PENNYLANE_CLIENT_SECRET / PENNYLANE_REDIRECT_URI ne sont
// pas tous renseignés, seule la connexion par jeton d'API est proposée.
type PennylaneConfig struct {
	// BaseURL : https://app.pennylane.com par défaut ; une autre adresse
	// permet de viser un environnement de test (PENNYLANE_BASE_URL).
	BaseURL      string
	ClientID     string
	ClientSecret string
	// RedirectURI : adresse de retour enregistrée chez Pennylane, ex.
	// https://api.welloresto.com/accounting-integrations/oauth/callback.
	RedirectURI string
}

// OAuthConfigured : l'application OAuth est complètement renseignée.
func (c PennylaneConfig) OAuthConfigured() bool {
	return c.ClientID != "" && c.ClientSecret != "" && c.RedirectURI != ""
}

func loadAccountingConfig() AccountingConfig {
	env := func(k, fallback string) string {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
		return fallback
	}
	return AccountingConfig{
		ReturnURL: env("ACCOUNTING_RETURN_URL", ""),
		Pennylane: PennylaneConfig{
			BaseURL:      strings.TrimRight(env("PENNYLANE_BASE_URL", "https://app.pennylane.com"), "/"),
			ClientID:     env("PENNYLANE_CLIENT_ID", ""),
			ClientSecret: env("PENNYLANE_CLIENT_SECRET", ""),
			RedirectURI:  env("PENNYLANE_REDIRECT_URI", ""),
		},
	}
}
