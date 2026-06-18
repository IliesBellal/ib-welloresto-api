package kiosk

// Config porte la configuration du module Kiosk, chargée depuis les
// variables d'environnement par internal/config (aliasé en
// config.KioskConfig, même pattern que config.UberEatsConfig) — ce type vit
// ici, pas dans internal/config, pour que ce module n'ait jamais besoin
// d'importer internal/config (qui dépend déjà de plusieurs modules,
// middleware en dépend aussi : importer config depuis kiosk créerait un
// cycle).
type Config struct {
	EnrollmentCodeTTLMinutes  int
	DeviceRefreshTokenTTLDays int
	AccessTokenTTLMinutes     int
	// Pepper sert à hacher les codes d'enrôlement, refresh tokens et à
	// signer les access tokens (HMAC-SHA256, même mécanisme que
	// security.HashPIN). Retombe sur PIN_PEPPER si KIOSK_TOKEN_PEPPER n'est
	// pas défini (voir internal/config/kiosk.go).
	Pepper string
}
