package kiosk

import "time"

// AccessTokenTTL : durée de vie de l'access token d'une borne. Constante, pas
// de variable d'environnement : 3 h, car chaque refresh fait tourner le
// refresh token (une ligne en base, une ligne de log) — à 15 min, c'était 96
// refresh par jour et par borne. Contrepartie : l'access token est auto-porteur
// (voir ValidateAccessToken), donc une borne révoquée garde ses routes REST
// jusqu'à son expiration.
const AccessTokenTTL = 180 * time.Minute

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
	// Pepper sert à hacher les codes d'enrôlement, refresh tokens et à
	// signer les access tokens (HMAC-SHA256, même mécanisme que
	// security.HashPIN). Retombe sur PIN_PEPPER si KIOSK_TOKEN_PEPPER n'est
	// pas défini (voir internal/config/kiosk.go).
	Pepper string
	// StripeTestMode gate l'endpoint dev POST /kiosk/terminal/test/present-payment-method
	// (docs/TERMINAL_SERVER_DRIVEN_CONTRACT.md) — calculé une fois dans
	// cmd/api/routes.go depuis cfg.Stripe.APIKey (préfixe sk_test_/rk_test_),
	// jamais lu directement ici pour ne pas importer internal/config (voir
	// commentaire de ce fichier).
	StripeTestMode bool
	// ScanNOrderBaseURL : SCANNORDER_BASE_URL, base du lien de suivi imprimé
	// en QR (qr_payload, tracking_url). Vide = helpers.DefaultScanNOrderBaseURL.
	ScanNOrderBaseURL string
}
