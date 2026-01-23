package googlemaps

import (
	"net/http"
)

type RouteHandler struct {
	service RouteService
}

func NewRouteHandler(service RouteService) *RouteHandler {
	return &RouteHandler{service: service}
}

func (h *RouteHandler) HandleGetRoute(w http.ResponseWriter, r *http.Request) {
	// Récupération des query params
	origin := r.URL.Query().Get("origin")
	destination := r.URL.Query().Get("destination")

	// Simulation d'un UserID (peut venir d'un BasicAuth JWT dans le header Authorization)
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		userID = "anonymous"
	}

	if origin == "" || destination == "" {
		http.Error(w, "Missing origin or destination", http.StatusBadRequest)
		return
	}

	// Appel du service
	jsonResponse, err := h.service.GetAndLogRoute(userID, origin, destination)
	if err != nil {
		http.Error(w, "Failed to fetch route", http.StatusInternalServerError)
		return
	}

	// Renvoi de la réponse (Proxy)
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonResponse)
}
