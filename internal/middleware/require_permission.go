package middleware

import (
	"net/http"

	"welloresto-api/internal/middleware/rbacobserve"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/permission"

	"github.com/go-chi/chi/v5"
)

// rbacObserver is nil unless EnableRBACObservation is called from
// SetupRoutes (gated by the RBAC_OBSERVE env var, default off). RequirePermission
// checks it on every call; a nil check is the entire cost of leaving
// observation disabled.
var rbacObserver *rbacobserve.Observer

// EnableRBACObservation wires an async RBAC decision observer into
// RequirePermission. Call once at startup; nil (the default, when
// RBAC_OBSERVE is unset or not "true") means RequirePermission observes
// nothing.
func EnableRBACObservation(o *rbacobserve.Observer) {
	rbacObserver = o
}

// observeDecision records a RequirePermission decision, best-effort and
// asynchronous (see rbacobserve.Observer.Observe) — never on the request's
// critical path. No-op when observation is disabled.
func observeDecision(r *http.Request, user *auth.UserLoginRow, key permission.Key, granted bool) {
	if rbacObserver == nil {
		return
	}
	rbacObserver.Observe(rbacobserve.Observation{
		MerchantID:    user.MerchantID,
		UserID:        user.UserID,
		PermissionKey: string(key),
		Route:         r.Method + " " + routePattern(r),
		Granted:       granted,
	})
}

// routePattern returns the chi route pattern ("/pos/status"), not the
// concrete request path, so observations aggregate across ids instead of
// fragmenting one row per resource instance. Falls back to the raw path if
// chi has not resolved a pattern (should not happen for a route that reached
// RequirePermission, kept only as a safety net).
func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	return r.URL.Path
}

// RequirePermission vérifie qu'un user détient le droit demandé, via
// user.Has(key) (internal/modules/auth/permissions.go) — qui bascule
// automatiquement entre les colonnes booléennes historiques et les droits du
// rôle selon que users_rights.role_id est renseigné ou non pour cet
// utilisateur.
//
// RBAC lot 2 : la signature est passée de RequirePermission(...PermissionFunc)
// (logique AND sur plusieurs prédicats combinables via AnyOf/AllOf) à
// RequirePermission(key permission.Key) — une seule clé du catalogue. Aucune
// route réelle ne combinait plusieurs prédicats au moment de la bascule (voir
// le résumé de livraison du lot), donc rien ne s'est perdu ; IsAdmin, qui
// n'est pas un droit du catalogue mais « détient tous les droits », a sa
// propre garde : RequireAdmin.
func RequirePermission(key permission.Key) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// ✅ Laisser passer les requêtes OPTIONS (preflight CORS)
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			user := GetUser(r)
			if user == nil {
				SetCORSHeaders(w, r)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}

			granted := user.Has(key)
			observeDecision(r, user, key, granted)

			if !granted {
				renderError(w, r, "access_denied", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// adminObservationKey is the conventional permission_key recorded for
// RequireAdmin decisions — see rbacobserve's package doc comment.
const adminObservationKey permission.Key = "__admin__"

// RequireAdmin restreint une route aux comptes administrateur. Distinct de
// RequirePermission à dessein : IsAdmin correspond à « détient tous les
// droits », pas à une permission.Key particulière du catalogue.
func RequireAdmin() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			user := GetUser(r)
			if user == nil {
				SetCORSHeaders(w, r)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}

			granted := IsAdmin(user)
			observeDecision(r, user, adminObservationKey, granted)

			if !granted {
				renderError(w, r, "access_denied", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Petit helper interne pour rester sec (DRY)
func renderError(w http.ResponseWriter, r *http.Request, code string, status int) {
	SetCORSHeaders(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":"` + code + `"}`))
}
