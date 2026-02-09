package users

import (
	"encoding/json"
	"fmt"
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
		http.Error(w, err.Error(), 500)
		return
	}

	if result == nil {
		http.Error(w, "user not found", 404)
		return
	}

	json.NewEncoder(w).Encode(result)
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
		http.Error(w, fmt.Sprintf(`{"status":"-3","error":"%s"}`, err.Error()), 400)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}
