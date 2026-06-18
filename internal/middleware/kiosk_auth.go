package middleware

import (
	"context"
	"net/http"
	"strings"

	"welloresto-api/internal/models"
)

// AuthenticatedKiosk identifie la borne physique authentifiée par KioskAuth.
// Définie ici (pas dans internal/modules/kiosk) afin que ce middleware n'ait
// jamais besoin d'importer le module kiosk : kiosk consomme menuService,
// ordersService, ordersLifeCycleService (incrément 2), qui importent eux-
// mêmes middleware — un import middleware -> kiosk créerait un cycle
// (middleware -> kiosk -> menu -> middleware). Le sens unique est donc
// kiosk -> middleware, jamais l'inverse.
type AuthenticatedKiosk struct {
	KioskID    string
	MerchantID string
}

const kioskContextKey contextKey = "kiosk"

// KioskAuthService est l'interface que kiosk.Service doit satisfaire pour
// être injecté dans ce middleware — distinct de middleware.AuthService, qui
// type le contexte avec un *auth.UserLoginRow (utilisateur humain), pas une
// borne physique.
type KioskAuthService interface {
	ValidateAccessToken(ctx context.Context, accessToken string) (*AuthenticatedKiosk, error)
}

// KioskAuth valide l'access token d'une borne (Authorization: Bearer <token>)
// et injecte la borne authentifiée dans le contexte. Distinct de
// middleware.Auth : aucun risque de régression sur les routes utilisateur.
func KioskAuth(service KioskAuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				SetCORSHeaders(w, r)
				models.SendErrorJSON(w, "kiosk", "auth", models.ErrKioskDeviceTokenInvalid)
				return
			}

			token := authHeader
			if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "bearer ") {
				token = authHeader[7:]
			}
			token = strings.TrimSpace(token)
			if token == "" {
				SetCORSHeaders(w, r)
				models.SendErrorJSON(w, "kiosk", "auth", models.ErrKioskDeviceTokenInvalid)
				return
			}

			authenticatedKiosk, err := service.ValidateAccessToken(r.Context(), token)
			if err != nil || authenticatedKiosk == nil {
				SetCORSHeaders(w, r)
				models.SendErrorJSON(w, "kiosk", "auth", models.ErrKioskDeviceTokenInvalid)
				return
			}

			ctx := context.WithValue(r.Context(), kioskContextKey, authenticatedKiosk)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetKiosk récupère la borne authentifiée injectée par KioskAuth depuis le
// contexte. Retourne nil si absente.
func GetKiosk(r *http.Request) *AuthenticatedKiosk {
	kiosk, _ := r.Context().Value(kioskContextKey).(*AuthenticatedKiosk)
	return kiosk
}
