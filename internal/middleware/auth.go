package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"welloresto-api/internal/modules/auth"
)

// Clé typée pour le contexte — évite les collisions avec d'autres valeurs du contexte
type contextKey string

const userContextKey contextKey = "authenticatedUser"

var ErrUnunauthenticated = errors.New("utilisateur non authentifié")

// AuthRepo est l'interface que ton authRepo doit satisfaire
// Cela permet de ne pas coupler le middleware directement au repo concret
type AuthRepo interface {
	GetUserByToken(ctx context.Context, token string) (*auth.UserLoginRow, error)
}

// Auth est le middleware d'authentification principal
// Il vérifie le token, récupère le user (via Redis), et l'injecte dans le contexte
func Auth(repo AuthRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Extraire le token du header Authorization
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"token manquant"}`, http.StatusUnauthorized)
				return
			}

			// 2. Vérifier le format "Bearer <token>"
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				http.Error(w, `{"error":"format token invalide"}`, http.StatusUnauthorized)
				return
			}
			token := parts[1]

			// 3. Récupérer le user (Redis d'abord, BDD ensuite — géré dans le repo)
			user, err := repo.GetUserByToken(r.Context(), token)
			if err != nil || user == nil {
				http.Error(w, `{"error":"token invalide ou expiré"}`, http.StatusUnauthorized)
				return
			}

			// 4. Injecter le user dans le contexte et passer au handler suivant
			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUser récupère le user injecté par le middleware depuis le contexte
// Retourne nil si l'utilisateur n'est pas authentifié
func GetUser(r *http.Request) *auth.UserLoginRow {
	user, _ := r.Context().Value(userContextKey).(*auth.UserLoginRow)
	return user
}

// UserFromContext récupère le user du contexte avec gestion d'erreur
// Utilisé par les services qui reçoivent un context.Context
func UserFromContext(ctx context.Context) (*auth.UserLoginRow, error) {
	user, ok := ctx.Value(userContextKey).(*auth.UserLoginRow)
	if !ok || user == nil {
		return nil, ErrUnunauthenticated
	}
	return user, nil
}

// MustGetUser récupère le user et envoie une erreur HTTP si absent
// Simplifie la gestion d'erreurs dans les handlers
func MustGetUser(w http.ResponseWriter, r *http.Request) (*auth.UserLoginRow, bool) {
	user := GetUser(r)
	if user == nil {
		http.Error(w, `{"error":"utilisateur non authentifié"}`, http.StatusUnauthorized)
		return nil, false
	}
	return user, true
}
