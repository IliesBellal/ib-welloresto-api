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
	//
	// Bascule "v2" (RBAC lot 2) : UserLoginRow a gagné RoleID/RoleSystemKey/
	// Permissions. Une entrée écrite avant ce déploiement désérialise ces
	// champs à leur zéro-value (nil/nil/vide), ce qui retombe silencieusement
	// sur les booléens historiques — inoffensif tant que role_id n'est jamais
	// renseigné, mais deviendrait un vrai incident de sécurité le jour où on
	// commence à assigner des rôles (un cache "v1" encore chaud referait
	// confiance aux anciens booléens au lieu du nouveau rôle). Changer le
	// préfixe rend orphelines toutes les entrées "v1" : elles expirent seules
	// (TTL 60 min, UserCacheTTL) sans jamais être relues, et la première
	// requête suivant le déploiement recharge systématiquement depuis SQL.
	// Ne JAMAIS construire cette clé avec un préfixe écrit en dur ailleurs
	// dans le code — toujours passer par cette constante.
	UserCachePrefix = "user:token:v2:"

	// Préfixe des clés Redis pour les merchants (scannorder)
	// Toutes les clés sont indexées par merchant_id (plus jamais par QR seul)
	// pour permettre l'invalidation par merchant — voir
	// redis.Client.InvalidateMerchantMenuCaches.
	ScannorderMerchant       = "scannorder:merchant:"
	ScannorderMerchantMenu   = "scannorder:merchant:menu:"
	ScannorderMerchantUpsell = "scannorder:merchant:upsell:"

	// Préfixe des clés Redis pour le menu Kiosk (clé : préfixe + merchantID + ":" + orderType)
	KioskMerchantMenu = "kiosk:merchant:menu:"

	// Durée de vie du cache pour les merchants
	ScannorderKioskMerchantMenuTTL = 10 * time.Minute

	// Durée de vie courte (pas d'invalidation active sur ce cache aujourd'hui :
	// is_open, prep time, etc. peuvent changer sans le vider) — plafonne la
	// fenêtre de statut périmé plutôt que de la laisser courir 60 min.
	ScannorderMerchantTTL = 2 * time.Minute

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

	// PasswordResetIPThrottlePrefix keys the per-IP counter on
	// POST /auth/forgot-password. Best-effort only: Redis is not a source of
	// truth here, the per-account limit enforced in SQL is (docs/PASSWORD_RESET.md).
	PasswordResetIPThrottlePrefix = "pwd_reset:ip:"
	PasswordResetIPThrottleTTL    = 1 * time.Hour
	PasswordResetIPThrottleMax    = 20

	// Préfixes des clés Redis pour la confirmation de création d'un nom en
	// doublon (produit/ingrédient/attribut) : le 1er appel avec un nom déjà
	// utilisé pose la clé et bloque (erreur "_with_retry"), un 2e appel
	// identique (même merchant + même nom) la trouve déjà posée et la
	// création est acceptée malgré le doublon.
	MenuProductNameConfirmPrefix   = "menu:product:name_confirm:"
	MenuComponentNameConfirmPrefix = "menu:component:name_confirm:"
	MenuAttributeNameConfirmPrefix = "menu:attribute:name_confirm:"
	MenuNameConfirmTTL             = 5 * time.Minute

	// Préfixe des clés portant le snapshot d'une preview d'import de produits.
	// La clé est scopée par marchand en plus du token : un token égaré ne doit
	// pas pouvoir être rejoué depuis un autre compte.
	//
	// 30 minutes : le wizard fait classer les libellés, mapper les taux de TVA
	// et arbitrer les collisions avant de valider. C'est un travail de
	// plusieurs minutes sur un menu complet, sans commune mesure avec les
	// 5 minutes d'une confirmation de doublon.
	MenuImportPreviewPrefix = "menu:import:preview:"
	MenuImportPreviewTTL    = 30 * time.Minute

	// Préfixe des clés portant le snapshot d'une preview d'import de clients.
	// Même convention que MenuImportPreviewPrefix : scopée par marchand en plus
	// du token, même TTL de 30 minutes (le wizard de dédup/arbitrage demande le
	// même ordre de grandeur de travail qu'un import de menu).
	CustomerImportPreviewPrefix = "customer:import:preview:"
	CustomerImportPreviewTTL    = 30 * time.Minute
)
