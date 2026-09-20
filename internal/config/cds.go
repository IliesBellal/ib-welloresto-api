package config

import "os"

// CDSConfig est une struct plate, indépendante de cds.Config (qui vit dans
// internal/modules/cds) — même raison que KioskConfig : un alias
// `type CDSConfig = cds.Config` créerait un cycle d'import dès que le module
// cds dépendrait d'un module important lui-même internal/config.
// routes.go fait la conversion explicite au moment de construire le service.
type CDSConfig struct {
	EnrollmentCodeTTLMinutes  int
	DeviceRefreshTokenTTLDays int
	AccessTokenTTLMinutes     int
	Pepper                    string
}

func loadCDSConfig() CDSConfig {
	pepper := os.Getenv("CDS_TOKEN_PEPPER")
	if pepper == "" {
		pepper = os.Getenv("PIN_PEPPER")
	}

	return CDSConfig{
		// 10 min par défaut, contre 15 pour le kiosk : le code CDS ne fait
		// que 6 chiffres (contrainte de saisie à la télécommande), son
		// espace est un million de fois plus petit. Réduire la fenêtre
		// pendant laquelle des codes sont valides réduit la surface
		// d'attaque — sans remplacer le rate limiting qui manque encore,
		// voir docs/audits/2026-09-19-enrollment-rate-limiting.md.
		EnrollmentCodeTTLMinutes:  getEnvInt("CDS_ENROLLMENT_CODE_TTL_MINUTES", 10),
		DeviceRefreshTokenTTLDays: getEnvInt("CDS_DEVICE_TOKEN_TTL_DAYS", 30),
		AccessTokenTTLMinutes:     getEnvInt("CDS_ACCESS_TOKEN_TTL_MINUTES", 15),
		Pepper:                    pepper,
	}
}
