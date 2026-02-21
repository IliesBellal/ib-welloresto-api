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
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	userID := chi.URLParam(r, "user_id")

	result, err := h.svc.GetUserLocation(ctx, token, userID)
	if err != nil {
		w.WriteHeader(500)
		models.SendJSON(w, "user", "location_error", map[string]string{"error": err.Error()})
		return
	}

	if result == nil {
		w.WriteHeader(404)
		models.SendJSON(w, "user", "location_error", map[string]string{"error": "user not found"})
		return
	}

	models.SendJSON(w, "user", "location", result)
}

func (h *UsersHandler) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	var req models.UpdatePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"status":"-2","error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	if err := h.svc.UpdatePassword(ctx, token, req.OldPassword, req.NewPassword); err != nil {
		models.SendJSON(w, "user", "update_password_error", map[string]string{"status": err.Error()})
		return
	}

	models.SendJSON(w, "user", "update_password", map[string]string{"status": "ok"})
}

func (h *UsersHandler) UpdateUserSettings(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	var req models.UserSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"status":"-2","error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	userID := chi.URLParam(r, "user_id")

	if err := h.svc.UpdateUserSettings(r.Context(), userID, token, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		models.SendJSON(w, "user", "update_settings_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "user", "update_settings", map[string]string{"status": "success"})
}
