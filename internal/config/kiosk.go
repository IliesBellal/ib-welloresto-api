package config

import (
	"os"
	"strconv"
)

// KioskConfig est une struct plate, indépendante de kiosk.Config (qui vit
// dans internal/modules/kiosk). Depuis l'incrément 2, le module kiosk
// importe menu/orders/order_life_cycle/upsell, qui importent eux-mêmes
// (transitivement) internal/config — un alias `type KioskConfig = kiosk.Config`
// créerait donc un cycle d'import (config -> kiosk -> menu -> ... -> config).
// routes.go fait la conversion explicite vers kiosk.Config au moment de
// construire le service.
type KioskConfig struct {
	EnrollmentCodeTTLMinutes  int
	DeviceRefreshTokenTTLDays int
	AccessTokenTTLMinutes     int
	Pepper                    string
}

func loadKioskConfig() KioskConfig {
	pepper := os.Getenv("KIOSK_TOKEN_PEPPER")
	if pepper == "" {
		pepper = os.Getenv("PIN_PEPPER")
	}

	return KioskConfig{
		EnrollmentCodeTTLMinutes:  getEnvInt("KIOSK_ENROLLMENT_CODE_TTL_MINUTES", 15),
		DeviceRefreshTokenTTLDays: getEnvInt("KIOSK_DEVICE_TOKEN_TTL_DAYS", 30),
		AccessTokenTTLMinutes:     getEnvInt("KIOSK_ACCESS_TOKEN_TTL_MINUTES", 15),
		Pepper:                    pepper,
	}
}

func getEnvInt(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
