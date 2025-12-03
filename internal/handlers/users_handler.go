package handlers

import (
	"encoding/json"
	"net/http"
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
