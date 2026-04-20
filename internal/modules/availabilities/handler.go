package availabilities

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type AvailabilitiesHandler struct {
	availabilitiesSvc *AvailabilitiesService
}

func NewAvailabilitiesHandler(svc *AvailabilitiesService) *AvailabilitiesHandler {
	return &AvailabilitiesHandler{
		availabilitiesSvc: svc,
	}
}

// GET /menu/availabilities
// Liste toutes les disponibilités pour le dashboard
func (h *AvailabilitiesHandler) GetAvailabilities(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "availabilities", "get_availabilities", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	availabilities, err := h.availabilitiesSvc.GetAvailabilitiesByMerchant(ctx)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "availabilities", "get_availabilities", map[string]string{"error": err.Error()})
		return
	}

	// Convertir en response objects
	response := make([]AvailabilityResponse, 0)
	for _, a := range availabilities {
		response = append(response, AvailabilityResponse{
			AvailabilityID:     a.AvailabilityID,
			Name:               a.Name,
			UnavailableMessage: a.UnavailableMessage,
			Enabled:            a.Enabled,
			CreatedAt:          a.CreatedAt,
			UpdatedAt:          a.UpdatedAt,
			ProductIDs:         a.ProductIDs,
			Schedules:          a.Schedules,
		})
	}

	models.SendJSON(w, http.StatusOK, "availabilities", "get_availabilities", map[string]interface{}{
		"availabilities": response,
	})
}

// POST /menu/availabilities
// Crée une nouvelle disponibilité
func (h *AvailabilitiesHandler) CreateAvailability(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "availabilities", "create_availability", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	var req CreateAvailabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "availabilities", "create_availability", map[string]string{"error": "invalid_body"})
		return
	}

	availability, err := h.availabilitiesSvc.CreateAvailability(ctx, req)
	if err != nil {
		models.SendJSON(w, http.StatusBadRequest, "availabilities", "create_availability", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusCreated, "availabilities", "create_availability", AvailabilityResponse{
		AvailabilityID:     availability.AvailabilityID,
		Name:               availability.Name,
		UnavailableMessage: availability.UnavailableMessage,
		Enabled:            availability.Enabled,
		CreatedAt:          availability.CreatedAt,
		UpdatedAt:          availability.UpdatedAt,
		ProductIDs:         availability.ProductIDs,
		Schedules:          availability.Schedules,
	})
}

// PATCH /menu/availabilities/{id}
// Met à jour une disponibilité existante
func (h *AvailabilitiesHandler) UpdateAvailability(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "availabilities", "update_availability", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	availabilityID := chi.URLParam(r, "id")

	var req UpdateAvailabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "availabilities", "update_availability", map[string]string{"error": "invalid_body"})
		return
	}

	availability, err := h.availabilitiesSvc.UpdateAvailability(ctx, availabilityID, req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			models.SendJSON(w, http.StatusNotFound, "availabilities", "update_availability", map[string]string{"error": "availability_not_found"})
		} else {
			models.SendJSON(w, http.StatusBadRequest, "availabilities", "update_availability", map[string]string{"error": err.Error()})
		}
		return
	}

	models.SendJSON(w, http.StatusOK, "availabilities", "update_availability", AvailabilityResponse{
		AvailabilityID:     availability.AvailabilityID,
		Name:               availability.Name,
		UnavailableMessage: availability.UnavailableMessage,
		Enabled:            availability.Enabled,
		CreatedAt:          availability.CreatedAt,
		UpdatedAt:          availability.UpdatedAt,
		ProductIDs:         availability.ProductIDs,
		Schedules:          availability.Schedules,
	})
}

// DELETE /menu/availabilities/{id}
// Supprime une disponibilité
func (h *AvailabilitiesHandler) DeleteAvailability(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "availabilities", "delete_availability", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	availabilityID := chi.URLParam(r, "id")

	err := h.availabilitiesSvc.DeleteAvailability(ctx, availabilityID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			models.SendJSON(w, http.StatusNotFound, "availabilities", "delete_availability", map[string]string{"error": "availability_not_found"})
		} else {
			models.SendJSON(w, http.StatusInternalServerError, "availabilities", "delete_availability", map[string]string{"error": err.Error()})
		}
		return
	}

	models.SendJSON(w, http.StatusOK, "availabilities", "delete_availability", map[string]interface{}{
		"status": "ok",
	})
}

// GET /menu/availabilities/{id}/check
// Vérifie la disponibilité d'un produit
func (h *AvailabilitiesHandler) CheckProductAvailability(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "availabilities", "check_product_availability", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	// Query parameters
	productID := r.URL.Query().Get("product_id")
	if strings.TrimSpace(productID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "availabilities", "check_product_availability", map[string]string{"error": "product_id_required"})
		return
	}

	// Récupérer le commerçant depuis le contexte
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		models.SendJSON(w, http.StatusUnauthorized, "availabilities", "check_product_availability", map[string]string{"error": "invalid_token"})
		return
	}

	// Vérifier la disponibilité
	isAvailable, err := h.availabilitiesSvc.IsProductAvailable(ctx, user.MerchantID, productID)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "availabilities", "check_product_availability", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "availabilities", "check_product_availability", ProductAvailabilityInfo{
		IsAvailable: isAvailable,
	})
}
