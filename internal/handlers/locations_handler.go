package handlers

import (
	"encoding/json"
	"net/http"
	"welloresto-api/internal/services"
)

// LocationsHandler handles orders endpoints
type LocationsHandler struct {
	locationsService *services.LocationsService
}

func NewLocationsHandler(locationsService *services.LocationsService) *LocationsHandler {
	return &LocationsHandler{
		locationsService: locationsService,
	}
}

func (h *LocationsHandler) GetLocations(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)

	resp, err := h.locationsService.GetLocations(r.Context(), token)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	json.NewEncoder(w).Encode(resp)
}
