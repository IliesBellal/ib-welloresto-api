package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

// Clé typée pour le contexte — évite les collisions avec d'autres valeurs du contexte
type contextKey string

const userContextKey contextKey = "authenticatedUser"

var ErrUnunauthenticated = errors.New("utilisateur non authentifié")

// AuthService est l'interface que ton authService doit satisfaire
// Cela permet de ne pas coupler le middleware directement au repo concret
type AuthService interface {
	GetUserByToken(ctx context.Context, token string) (*auth.UserLoginRow, error)
	UpdateMFAStatus(ctx context.Context, userID string, status string) error
}

// Auth est le middleware d'authentification principal
// Il vérifie le token, récupère le user (via Redis), et l'injecte dans le contexte
func Auth(service AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// ✅ IMPORTANT : Laisser passer les requêtes OPTIONS (preflight CORS)
			// Le middleware CORS doit pouvoir répondre sans authentification requise
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// 1. Extraire le header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				SetCORSHeaders(w, r)
				http.Error(w, `{"error":"token manquant"}`, http.StatusUnauthorized)
				return
			}

			// 2. Logique hybride : On nettoie et on extrait
			token := authHeader

			// Si ça commence par "Bearer " (insensible à la casse)
			if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "bearer ") {
				token = authHeader[7:]
			}

			/*
				Utiliser ce code une fois que toutes les applications utilisent "Bearer " comme préfixe :
				// 2. Vérifier le format "Bearer <token>"
				parts := strings.SplitN(authHeader, " ", 2)
				if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
					http.Error(w, `{"error":"format token invalide - are you using bearer as prefix ?"}`, http.StatusUnauthorized)
					return
				}
				token := parts[1]
			*/
			// On nettoie les espaces restants au cas où
			token = strings.TrimSpace(token)

			// Sécurité : on vérifie que le token n'est pas devenu vide après le nettoyage
			if token == "" {
				SetCORSHeaders(w, r)
				http.Error(w, `{"error":"format token invalide"}`, http.StatusUnauthorized)
				return
			}

			// 3. Récupérer le user (inchangé)
			user, err := service.GetUserByToken(r.Context(), token)
			if err != nil || user == nil {
				SetCORSHeaders(w, r)
				http.Error(w, `{"error":"token invalide ou expiré"}`, http.StatusUnauthorized)
				return
			}

			// --- NOUVELLE LOGIQUE MFA ---
			// On vérifie si l'accès demande le MFA (ex: via un header ou le path)
			isBackoffice := r.Header.Get("X-App-Source") == "backoffice"

			if isBackoffice && user.MFAType != nil && (user.MFAStatus == nil || *user.MFAStatus != models.MFAStatusVerified) {
				// On laisse passer UNIQUEMENT vers l'endpoint de vérification MFA
				if r.URL.Path != "/auth/verify" {
					// ✅ IMPORTANT : Ajouter les headers CORS AVANT d'envoyer la réponse
					// Sinon le navigateur bloque la réponse avec une erreur CORS
					service.UpdateMFAStatus(r.Context(), user.UserID, models.MFAStatusPending)
					user.MFAStatus = helpers.StringPtr(models.MFAStatusPending) // Mettre à jour le statut dans le user pour les handlers en aval
					SetCORSHeaders(w, r)
					models.SendErrorJSON(w, "auth", "login", models.ErrMFARequired)
					return
				}
			}

			// 4. Injecter le user (inchangé)
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
