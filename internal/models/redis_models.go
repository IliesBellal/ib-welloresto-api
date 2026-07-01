package models

import (
	"time"
)

const (
	// Durée de vie du cache : 60 minutes
	// Après 60 min, le prochain appel refera la requête SQL et rafraîchira le cache
	UserCacheTTL = 60 * time.Minute

	// Préfixe des clés Redis pour les users
	// Permet d'identifier facilement les clés dans Redis
	UserCachePrefix = "user:token:"

	// Préfixe des clés Redis pour les merchants (scannorder)
	// Permet d'identifier facilement les clés dans Redis
	ScannorderMerchant       = "scannorder:merchant:"
	ScannorderMerchantMenu   = "scannorder:merchant:menu:"
	ScannorderMerchantUpsell = "scannorder:merchant:upsell:"

	// Durée de vie du cache pour les merchants
	ScannorderMerchantMenuTTL = 10 * time.Minute

	// Durée de vie du cache pour les merchants
	ScannorderMerchantTTL = 60 * time.Minute

	// Préfixe des clés Redis pour les orders
	OrdersCachePrefix = "order:"

	// Durée de vie du cache pour les orders
	OrdersCacheTTL = 5 * time.Minute

	// Préfixe des clés Redis pour les notifications
	WebhookUberEatsEventPrefix = "webhook:uber:event:"

	WebhookDeliverooEventPrefix = "webhook:deliveroo:event:"

	WebhookDeliverooLocationPrefix = "webhook:deliveroo:location:"

	WebhookdeliveroolocationTTL = 24 * time.Hour

	// Durée de vie du cache pour les notifications d'Uber Eats
	WebhookUberEatsEventTTL = 3 * time.Hour

	// PINLockoutPrefix keys the per-tablet brute-force counter (keyed on anchor token).
	PINLockoutPrefix = "pin:lockout:"
	PINLockoutTTL    = 1 * time.Hour

	MFACachePrefix = "mfa_otp:"

	OTPCacheTTL = 5 * time.Minute

	VerificationCachePrefix = "verify:"

	VerificationCacheTTL = 15 * time.Minute
)
