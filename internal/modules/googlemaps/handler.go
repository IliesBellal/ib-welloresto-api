package googlemaps

import (
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

type RouteHandler struct {
	service RouteService
}

func NewRouteHandler(service RouteService) *RouteHandler {
	return &RouteHandler{service: service}
}

func (h *RouteHandler) HandleGetRoute(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "googlemaps", "get_route", map[string]string{"error": "invalid_token"})
		return
	}

	// Récupération des query params
	origin := r.URL.Query().Get("origin")
	destination := r.URL.Query().Get("destination")

	// Simulation d'un UserID (peut venir d'un BasicAuth JWT dans le header Authorization)
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		userID = "anonymous"
	}

	if origin == "" || destination == "" {
		models.SendJSON(w, http.StatusBadRequest, "googlemaps", "get_route", map[string]string{"error": "missing_parameter"})
		return
	}

	// Appel du service
	jsonResponse, err := h.service.GetAndLogRoute(userID, origin, destination)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "googlemaps", "get_route", map[string]string{"error": "failed_to_fetch_route"})
		return
	}

	// Renvoi de la réponse (Proxy)
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonResponse)
}
