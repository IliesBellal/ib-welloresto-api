package users

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type UsersHandler struct {
	svc *UsersService
}

func NewUsersHandler(s *UsersService) *UsersHandler {
	return &UsersHandler{svc: s}
}

func (h *UsersHandler) GetUserLocation(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "user", "location", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	userID := chi.URLParam(r, "user_id")

	result, err := h.svc.GetUserLocation(ctx, token, userID)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "user", "location", map[string]string{"error": err.Error()})
		return
	}

	if result == nil {
		models.SendJSON(w, http.StatusNotFound, "user", "location", map[string]string{"error": "user not found"})
		return
	}

	models.SendJSON(w, http.StatusOK, "user", "location", result)
}

func (h *UsersHandler) SetUserLocation(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "user", "location", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	var req models.UpdateLocationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "user", "update_location", map[string]string{"error": "invalid_request"})
		return
	}

	err := h.svc.SetUserLocation(ctx, token, req)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "user", "location", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "user", "location", map[string]string{"status": "success"})
}

func (h *UsersHandler) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "user", "update_password", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	var req models.UpdatePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "user", "update_password", map[string]string{"error": "invalid_request"})
		return
	}

	if err := h.svc.UpdatePassword(ctx, token, req.OldPassword, req.NewPassword); err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "user", "update_password", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "user", "update_password", map[string]string{"status": "success"})
}

func (h *UsersHandler) UpdateUserSettings(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "user", "update_settings", map[string]string{"error": "missing_token"})
		return
	}

	var req models.UserSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "user", "update_settings", map[string]string{"error": "invalid_request"})
		return
	}

	userID := chi.URLParam(r, "user_id")

	if err := h.svc.UpdateUserSettings(r.Context(), userID, token, &req); err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "user", "update_settings", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "user", "update_settings", map[string]string{"status": "success"})
}

func (h *UsersHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := h.svc.GetProfile(r.Context())
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "user", "get_profile", map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(profile)
}

func (h *UsersHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req models.UpdateUserProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "user", "update_profile", map[string]string{"error": "invalid_request"})
		return
	}

	if err := h.svc.UpdateProfile(r.Context(), &req); err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "user", "update_profile", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "user", "update_profile", map[string]string{"status": "success"})
}
