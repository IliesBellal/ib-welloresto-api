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
		models.SendErrorJSON(w, "locations", "get", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "locations", "get", resp)
}

func (h *LocationsHandler) CreateTable(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "locations", "create_table", map[string]string{"error": "missing_token"})
		return
	}

	floorID := chi.URLParam(r, "floor_id")
	if floorID == "" {
		models.SendJSON(w, http.StatusBadRequest, "locations", "create_table", map[string]string{"error": "missing_floor_id"})
		return
	}

	var payload CreateTableRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "locations", "create_table", map[string]string{"error": "invalid_body"})
		return
	}

	result, err := h.locationsService.CreateTable(r.Context(), token, floorID, payload)
	if err != nil {
		models.SendErrorJSON(w, "locations", "create_table", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "locations", "create_table", result)
}

func (h *LocationsHandler) UpdateTable(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "locations", "update_table", map[string]string{"error": "missing_token"})
		return
	}

	locationID := chi.URLParam(r, "location_id")
	if locationID == "" {
		models.SendJSON(w, http.StatusBadRequest, "locations", "update_table", map[string]string{"error": "missing_location_id"})
		return
	}

	var payload UpdateTableRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "locations", "update_table", map[string]string{"error": "invalid_body"})
		return
	}

	result, err := h.locationsService.UpdateTable(r.Context(), token, locationID, payload)
	if err != nil {
		models.SendErrorJSON(w, "locations", "update_table", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "locations", "update_table", result)
}

func (h *LocationsHandler) DeleteTable(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "locations", "delete_table", map[string]string{"error": "missing_token"})
		return
	}

	locationID := chi.URLParam(r, "location_id")
	if locationID == "" {
		models.SendJSON(w, http.StatusBadRequest, "locations", "delete_table", map[string]string{"error": "missing_location_id"})
		return
	}

	result, err := h.locationsService.DeleteTable(r.Context(), token, locationID)
	if err != nil {
		models.SendErrorJSON(w, "locations", "delete_table", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "locations", "delete_table", result)
}

func (h *LocationsHandler) CreateFloor(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "locations", "create_floor", map[string]string{"error": "missing_token"})
		return
	}

	var payload FloorCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "locations", "create_floor", map[string]string{"error": "invalid_body"})
		return
	}

	if strings.TrimSpace(payload.Name) == "" {
		models.SendJSON(w, http.StatusBadRequest, "locations", "create_floor", map[string]string{"error": "missing_name"})
		return
	}

	result, err := h.locationsService.CreateFloor(r.Context(), token, payload)
	if err != nil {
		models.SendErrorJSON(w, "locations", "create_floor", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "locations", "create_floor", result)
}

func (h *LocationsHandler) UpdateFloor(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "locations", "update_floor", map[string]string{"error": "missing_token"})
		return
	}

	floorID := chi.URLParam(r, "floor_id")
	if floorID == "" {
		models.SendJSON(w, http.StatusBadRequest, "locations", "update_floor", map[string]string{"error": "missing_floor_id"})
		return
	}

	var payload FloorUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "locations", "update_floor", map[string]string{"error": "invalid_body"})
		return
	}

	if strings.TrimSpace(payload.Name) == "" {
		models.SendJSON(w, http.StatusBadRequest, "locations", "update_floor", map[string]string{"error": "missing_name"})
		return
	}

	result, err := h.locationsService.UpdateFloor(r.Context(), token, floorID, payload)
	if err != nil {
		models.SendErrorJSON(w, "locations", "update_floor", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "locations", "update_floor", result)
}

func (h *LocationsHandler) DeleteFloor(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "locations", "delete_floor", map[string]string{"error": "missing_token"})
		return
	}

	floorID := chi.URLParam(r, "floor_id")
	if floorID == "" {
		models.SendJSON(w, http.StatusBadRequest, "locations", "delete_floor", map[string]string{"error": "missing_floor_id"})
		return
	}

	result, err := h.locationsService.DeleteFloor(r.Context(), token, floorID)
	if err != nil {
		models.SendErrorJSON(w, "locations", "delete_floor", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "locations", "delete_floor", result)
}
