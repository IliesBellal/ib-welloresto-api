package middleware

import (
	"net/http"

	"welloresto-api/internal/modules/auth"
)

// PermissionFunc est une fonction qui teste un droit sur un User
// C'est ce type qu'on passera à RequirePermission
type PermissionFunc func(user *auth.UserLoginRow) bool

// RequirePermission vérifie qu'un user possède un ou plusieurs droits
// Tous les droits passés doivent être vrais (logique AND)
func RequirePermission(permissions ...PermissionFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// ✅ Laisser passer les requêtes OPTIONS (preflight CORS)
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			user := GetUser(r)
			if user == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}

			for _, hasPermission := range permissions {
				if !hasPermission(user) {
					// Logique spécifique pour les vérifications d'identité
					if !IsEmailVerified(user) {
						renderError(w, "EMAIL_VERIFICATION_REQUIRED", http.StatusForbidden)
						return
					}
					if user.Rights.Admin && !IsTelVerified(user) {
						renderError(w, "TEL_VERIFICATION_REQUIRED", http.StatusForbidden)
						return
					}

					renderError(w, "access_denied", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Petit helper interne pour rester sec (DRY)
func renderError(w http.ResponseWriter, code string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":"` + code + `"}`))
}
