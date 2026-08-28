package productionprofiles

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

type Handler struct {
	service *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{service: svc}
}

// GET /production-profiles
func (h *Handler) ListProfiles(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "production_profiles", "list", map[string]string{"error": "missing_token"})
		return
	}

	profiles, err := h.service.ListProfiles(r.Context(), token)
	if err != nil {
		models.SendErrorJSON(w, "production_profiles", "list", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "production_profiles", "list", profiles)
}

// GET /production-profiles/{id}
func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "production_profiles", "get", map[string]string{"error": "missing_token"})
		return
	}

	profileID := chi.URLParam(r, "id")
	if strings.TrimSpace(profileID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "production_profiles", "get", map[string]string{"error": "missing_profile_id"})
		return
	}

	profile, err := h.service.GetProfile(r.Context(), token, profileID)
	if err != nil {
		models.SendErrorJSON(w, "production_profiles", "get", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "production_profiles", "get", profile)
}

// POST /production-profiles
func (h *Handler) CreateProfile(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "production_profiles", "create", map[string]string{"error": "missing_token"})
		return
	}

	var req CreateProductionProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "production_profiles", "create", map[string]string{"error": "invalid_request"})
		return
	}

	profile, err := h.service.CreateProfile(r.Context(), token, &req)
	if err != nil {
		models.SendErrorJSON(w, "production_profiles", "create", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "production_profiles", "create", profile)
}

// PATCH /production-profiles/{id}
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "production_profiles", "update", map[string]string{"error": "missing_token"})
		return
	}

	profileID := chi.URLParam(r, "id")
	if strings.TrimSpace(profileID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "production_profiles", "update", map[string]string{"error": "missing_profile_id"})
		return
	}

	var req UpdateProductionProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "production_profiles", "update", map[string]string{"error": "invalid_request"})
		return
	}

	profile, err := h.service.UpdateProfile(r.Context(), token, profileID, &req)
	if err != nil {
		models.SendErrorJSON(w, "production_profiles", "update", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "production_profiles", "update", profile)
}

// DELETE /production-profiles/{id}
func (h *Handler) DeleteProfile(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "production_profiles", "delete", map[string]string{"error": "missing_token"})
		return
	}

	profileID := chi.URLParam(r, "id")
	if strings.TrimSpace(profileID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "production_profiles", "delete", map[string]string{"error": "missing_profile_id"})
		return
	}

	if err := h.service.DeleteProfile(r.Context(), token, profileID); err != nil {
		models.SendErrorJSON(w, "production_profiles", "delete", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PUT /production-profiles/{id}/products
func (h *Handler) ReplaceProducts(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "production_profiles", "replace_products", map[string]string{"error": "missing_token"})
		return
	}

	profileID := chi.URLParam(r, "id")
	if strings.TrimSpace(profileID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "production_profiles", "replace_products", map[string]string{"error": "missing_profile_id"})
		return
	}

	var req ReplaceProductsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "production_profiles", "replace_products", map[string]string{"error": "invalid_request"})
		return
	}

	if err := h.service.ReplaceProducts(r.Context(), token, profileID, req); err != nil {
		models.SendErrorJSON(w, "production_profiles", "replace_products", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "production_profiles", "replace_products", map[string]string{"status": "success", "message": "products updated"})
}
