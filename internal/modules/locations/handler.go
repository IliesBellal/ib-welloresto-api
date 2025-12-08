package locations

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

// LocationsHandler handles orders endpoints
type LocationsHandler struct {
	locationsService *LocationsService
}

func NewLocationsHandler(locationsService *LocationsService) *LocationsHandler {
	return &LocationsHandler{
		locationsService: locationsService,
	}
}

func (h *LocationsHandler) GetLocations(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	resp, err := h.locationsService.GetLocations(r.Context(), token)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (h *LocationsHandler) UpdateLocationCoordinates(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}
	ctx := r.Context()

	locationID := chi.URLParam(r, "location_id")

	var payload models.UpdateLocationCoordinatesRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", 400)
		return
	}

	result, err := h.locationsService.UpdateLocationCoordinates(ctx, token, locationID, payload.X, payload.Y)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	json.NewEncoder(w).Encode(result)
}
