package handlers

import (
	"encoding/json"
	"net/http"
	"welloresto/internal/services"
)

type AuthHandler struct {
	svc *services.AuthService
}

func NewAuthHandler(s *services.AuthService) *AuthHandler {
	return &AuthHandler{svc: s}
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {

	app := r.URL.Query().Get("app")
	device := r.URL.Query().Get("device_id")
	user := r.URL.Query().Get("user")
	pwd := r.URL.Query().Get("pwd")
	tok := r.URL.Query().Get("token")

	resp, err := h.svc.Login(r.Context(), app, device, user, pwd, tok)
	if err != nil {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": 99,
			"data": map[string]interface{}{
				"status": "error",
				"error": err.Error(),
			},
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id": 99,
		"data": resp,
	})
}
