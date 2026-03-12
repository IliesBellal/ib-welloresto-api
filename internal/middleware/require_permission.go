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
			user := GetUser(r)
			if user == nil {
				// Ne devrait pas arriver si Auth() est appliqué avant
				http.Error(w, `{"error":"non authentifié"}`, http.StatusUnauthorized)
				return
			}

			// Vérifier chaque permission requise
			for _, hasPermission := range permissions {
				if !hasPermission(user) {
					http.Error(w, `{"error":"accès refusé"}`, http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
