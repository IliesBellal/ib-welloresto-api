package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
)

// suspendedReadOnlyExemptPrefixes — LOT B B2b-2 (§7.5) : "exports fiscaux et
// clôture de caisse restent accessibles (jamais bloqués, même suspendu)",
// plus the routes a suspended merchant needs to actually get itself
// unsuspended (billing) and the internal staff routes that manage merchants
// regardless of their own suspension (admin). A judgment call, not an
// exhaustive audit of every route in this API — see docs/decisions.md for
// the reasoning and an explicit invitation to review/extend this list.
//
// LOT B F1 (docs/decisions.md) — CORRECTIF IMPORTANT : ces préfixes portaient
// tous un "/v1/" en tête depuis leur toute première écriture (B2b-2), qui ne
// correspond à AUCUNE route réelle de ce dépôt — cmd/api/routes.go ne monte
// qu'un tout petit groupe (/signup, /public, /auth/google,
// /merchants/{id}/onboarding) sous r.Route("/v1", ...) ; /admin, /billing,
// /pos, /accounting, /analytics, /orders, /bookings, /cash_register sont
// tous montés directement à la racine du routeur. Conséquence réelle,
// confirmée en tapant directement sur le serveur staging déployé (401 sur
// "/admin/overrides" en POST sans /v1, 404 AVEC /v1) : cette liste
// d'exemptions n'a jamais correspondu à une seule requête réelle depuis son
// écriture — un marchand suspended se voyait bloqué même sur les exports
// fiscaux et la clôture de caisse, l'exact inverse de ce que B2b-2 devait
// garantir (§7.5/§7.6). Corrigé ici : tous les préfixes/chemins ci-dessous
// sans "/v1".
var suspendedReadOnlyExemptPrefixes = []string{
	"/admin",
	"/billing",
	"/pos/reports",
	"/pos/accounting",
	"/accounting",
	// LOT B F1 : /analytics et ses 4 sous-groupes enregistrés séparément
	// (/analytics/merchants, /cancellations, /clients, /upsell) partagent
	// tous ce préfixe — un seul suffit. Ce sont des POST utilisés uniquement
	// pour porter des critères de filtre, pas pour écrire.
	"/analytics",
}

// suspendedReadOnlyExemptExactPaths — LOT B F1 (docs/decisions.md) : POST
// identifiés en revue comme des consultations (verbe POST utilisé pour
// porter des critères de filtre/recherche, jamais pour écrire). Correspondance
// EXACTE et non préfixe : les routes sœurs du même groupe (/orders/create,
// /bookings/create, etc.) doivent rester bloquées.
//
// /orders/{id}/invoice/email-sms est délibérément absent de cette liste :
// ce n'est pas une consultation, ça déclenche une communication vers le
// client final du restaurant — un effet externe incompatible avec la
// suspension. Laissé bloqué, explicitement.
var suspendedReadOnlyExemptExactPaths = []string{
	"/orders/pricing",
	"/orders/upsell",
	"/orders/list",
	"/orders/history",
	"/cash_register/history",
	"/bookings",
	"/bookings/",
}

// suspendedReadOnlyExemptSuffixes — cash register closing specifically
// (never its opening, which gets its own dedicated refusal message instead,
// see cash_registers.Service.OpenCashRegister).
var suspendedReadOnlyExemptSuffixes = []string{"/close", "/enclose"}

func isSuspendedReadOnlyExempt(path string) bool {
	for _, p := range suspendedReadOnlyExemptPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	for _, p := range suspendedReadOnlyExemptExactPaths {
		if path == p {
			return true
		}
	}
	if strings.HasPrefix(path, "/cash_register") {
		for _, suf := range suspendedReadOnlyExemptSuffixes {
			if strings.HasSuffix(path, suf) {
				return true
			}
		}
	}
	return false
}

// RequireNotSuspended — LOT B B2b-2 : "lecture seule" pour un marchand
// suspended, appliqué au niveau de middleware.Auth (le seul point partagé
// par les ~40 groupes de routes authentifiées de ce dépôt — voir
// docs/decisions.md) plutôt que route par route. Ne bloque que les requêtes
// mutatives (méthode != GET/OPTIONS/HEAD) d'un marchand suspended, hors
// exemptions ci-dessus. Une erreur de lecture DB ici laisse passer la
// requête (fail-open) — un faux-négatif sur ce garde-fou est préférable à
// transformer un souci d'infrastructure en panne totale de l'API pour tout
// le monde.
func RequireNotSuspended(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			if isSuspendedReadOnlyExempt(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			user := GetUser(r)
			if user == nil {
				// Pas encore authentifié à ce stade (ou route publique) —
				// rien à garder ici, Auth (exécuté avant dans la même
				// chaîne) a déjà géré le cas non-authentifié.
				next.ServeHTTP(w, r)
				return
			}

			suspended, err := isMerchantSuspended(r.Context(), db, user.MerchantID)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			if suspended {
				renderError(w, r, "merchant_suspended_read_only", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isMerchantSuspended — lecture toujours fraîche, jamais via
// l'utilisateur authentifié mis en cache Redis (models.UserCacheTTL, 60
// minutes) : un marchand tout juste régularisé (bouton "réessayer
// maintenant") ne doit pas rester bloqué en lecture seule par un cache
// périmé.
func isMerchantSuspended(ctx context.Context, db *sql.DB, merchantID string) (bool, error) {
	var status sql.NullString
	err := db.QueryRowContext(ctx, `SELECT status FROM subscriptions WHERE merchant_id = $1`, merchantID).Scan(&status)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return status.Valid && status.String == "suspended", nil
}
