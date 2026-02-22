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

	models.SendJSON(w, http.StatusOK, "user", "update_password", map[string]string{"status": "ok"})
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
