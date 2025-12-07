package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"welloresto-api/internal/models"
	"welloresto-api/internal/services"

	"github.com/go-chi/chi/v5"
)

type UsersHandler struct {
	svc *services.UsersService
}

func NewUsersHandler(s *services.UsersService) *UsersHandler {
	return &UsersHandler{svc: s}
}

func (h *UsersHandler) GetUserLocation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)
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
	ctx := r.Context()

	token := extractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

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

	token := extractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	var req models.UserSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"status":"-2","error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	if err := h.svc.UpdateUserSettings(r.Context(), token, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}
