package middleware

import (
	"context"
	"net/http"
	"strings"

	"welloresto-api/internal/models"
)

// AuthenticatedCDS identifie l'écran d'affichage client authentifié par
// CDSAuth. Défini ici (pas dans internal/modules/cds) pour la même raison que
// AuthenticatedKiosk : ce middleware ne doit jamais importer le module cds,
// qui importe lui-même middleware. Le sens unique est cds -> middleware.
type AuthenticatedCDS struct {
	DisplayID  string
	MerchantID string
}

const cdsContextKey contextKey = "cds"

// CDSAuthService est l'interface que cds.Service doit satisfaire pour être
// injecté dans ce middleware — distincte de KioskAuthService : un token CDS
// ne doit jamais authentifier une borne, ni l'inverse. C'est la garantie,
// au niveau du typage, qu'un écran passif ne peut pas atteindre
// POST /kiosk/orders (voir CDS_DECISIONS.md D10).
type CDSAuthService interface {
	ValidateAccessToken(ctx context.Context, accessToken string) (*AuthenticatedCDS, error)
}

// CDSAuth valide l'access token d'un écran (Authorization: Bearer <token>)
// et injecte l'écran authentifié dans le contexte.
func CDSAuth(service CDSAuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				SetCORSHeaders(w, r)
				models.SendErrorJSON(w, "cds", "auth", models.ErrCDSDeviceTokenInvalid)
				return
			}

			token := authHeader
			if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "bearer ") {
				token = authHeader[7:]
			}
			token = strings.TrimSpace(token)
			if token == "" {
				SetCORSHeaders(w, r)
				models.SendErrorJSON(w, "cds", "auth", models.ErrCDSDeviceTokenInvalid)
				return
			}

			authenticatedCDS, err := service.ValidateAccessToken(r.Context(), token)
			if err != nil || authenticatedCDS == nil {
				SetCORSHeaders(w, r)
				models.SendErrorJSON(w, "cds", "auth", models.ErrCDSDeviceTokenInvalid)
				return
			}

			ctx := context.WithValue(r.Context(), cdsContextKey, authenticatedCDS)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetCDS récupère l'écran authentifié injecté par CDSAuth depuis le contexte.
// Retourne nil si absent.
func GetCDS(r *http.Request) *AuthenticatedCDS {
	cds, _ := r.Context().Value(cdsContextKey).(*AuthenticatedCDS)
	return cds
}
