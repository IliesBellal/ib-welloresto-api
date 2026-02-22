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
		models.SendJSON(w, http.StatusUnauthorized, "locations", "get", map[string]string{"error": "missing_token"})
		return
	}

	resp, err := h.locationsService.GetLocations(r.Context(), token)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "locations", "get", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "locations", "get", resp)
}

func (h *LocationsHandler) UpdateLocationCoordinates(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "locations", "update_coordinates", map[string]string{"error": "missing_token"})
		return
	}
	ctx := r.Context()

	locationID := chi.URLParam(r, "location_id")

	var payload models.UpdateLocationCoordinatesRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "locations", "update_coordinates", map[string]string{"error": "invalid_body"})
		return
	}

	result, err := h.locationsService.UpdateLocationCoordinates(ctx, token, locationID, payload.X, payload.Y)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "locations", "update_coordinates", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "locations", "update_coordinates", result)
}
