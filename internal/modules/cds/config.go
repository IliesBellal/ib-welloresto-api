package cds

// Config porte la configuration du module CDS, chargée depuis les variables
// d'environnement par internal/config. Ce type vit ici, pas dans
// internal/config, pour que ce module n'ait jamais besoin d'importer
// internal/config (qui dépend déjà de plusieurs modules) — même raisonnement
// que kiosk.Config.
type Config struct {
	// EnrollmentCodeTTLMinutes — volontairement court (10 min par défaut).
	// Le code CDS ne fait que 6 chiffres (contrainte de saisie à la
	// télécommande, voir CDS_DECISIONS.md D14/D16), soit un espace de 10^6
	// contre 32^8 pour le kiosk. Le TTL ne limite pas le débit des
	// tentatives — seul un rate limiting le ferait, et il n'existe pas
	// encore dans cette API (voir
	// docs/audits/2026-09-19-enrollment-rate-limiting.md) — mais il réduit
	// le nombre de codes valides simultanément, donc la taille de la cible.
	EnrollmentCodeTTLMinutes  int
	DeviceRefreshTokenTTLDays int
	AccessTokenTTLMinutes     int
	// Pepper sert à hacher les codes d'enrôlement et refresh tokens, et à
	// signer les access tokens (HMAC-SHA256, même mécanisme que
	// security.HashPIN). Retombe sur PIN_PEPPER si CDS_TOKEN_PEPPER n'est
	// pas défini.
	Pepper string
}

// maxActiveDisplays est le plafond technique d'écrans par merchant
// (CDS_DECISIONS.md D4). Constante Go et non colonne en base : ce n'est pas
// un quota d'abonnement — le CDS est gratuit — et il ne doit pas en avoir
// l'apparence. Compté sur status <> 'revoked' : il n'existe pas d'état
// désactivé (D15), donc la seule façon de libérer une place est de révoquer.
const maxActiveDisplays = 4

// scheduledLookaheadMinutes — une commande planifiée n'apparaît sur l'écran
// qu'à H-1 (CDS_DECISIONS.md D12).
//
// Ce seuil diverge volontairement des deux autres fenêtres du codebase, qui
// répondent à des questions différentes :
//   - distributiontime/estimate.go : 90 min, pour savoir quelle charge
//     cuisine entre dans le calcul d'une estimation ;
//   - cash_registers/repository.go : 0 min (now > estimated_ready), pour
//     savoir ce qui entre dans une clôture de caisse.
//
// Ici la question est « à partir de quand un client a-t-il une raison de
// regarder l'écran », d'où une valeur propre.
const scheduledLookaheadMinutes = 60
