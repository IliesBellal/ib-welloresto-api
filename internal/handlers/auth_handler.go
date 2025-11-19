package handlers

import (
	"encoding/json"
	"net/http"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/services"
)

type AuthHandler struct {
	svc *services.AuthService
}

func NewAuthHandler(s *services.AuthService) *AuthHandler {
	return &AuthHandler{svc: s}
}

// Can be used with user and pwd, with token in get, or token in authorization
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {

	app := r.URL.Query().Get("app")
	device := r.URL.Query().Get("device_id")
	user := r.URL.Query().Get("user")
	pwd := r.URL.Query().Get("pwd")
	token := middleware.GetToken(r)

	resp, err := h.svc.Login(r.Context(), app, device, user, pwd, token)
	if err != nil {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": 99,
			"data": map[string]interface{}{
				"status": "error",
				"error":  err.Error(),
			},
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":   99,
		"data": resp,
	})
}
