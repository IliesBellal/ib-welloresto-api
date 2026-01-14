package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
)

type AuthHandler struct {
	svc AuthService
}

func NewAuthHandler(s AuthService) *AuthHandler {
	return &AuthHandler{svc: s}
}

// Can be used with user and pwd, with token in get, or token in authorization
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {

	token := helpers.ExtractToken(r)
	// Parse JSON body
	var req LoginRequestPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && token == "" {
		http.Error(w, `{"status":"-2","error":"invalid payload"}`, 400)
		return
	}

	resp, err := h.svc.Login(r.Context(), req, token)
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

func (h *AuthHandler) CheckAppVersion(w http.ResponseWriter, r *http.Request) {
	// Get token (header or ?token= fallback)
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}

	ctx := r.Context()

	// Parse JSON body
	var req CheckAppVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"status":"-2","error":"invalid payload"}`, 400)
		return
	}

	version := strings.TrimSpace(req.Version)
	appName := strings.TrimSpace(req.App)
	if version == "" || appName == "" {
		http.Error(w, `{"status":"-2","error":"missing fields"}`, 400)
		return
	}

	resp, err := h.svc.CheckAppVersion(ctx, token, version, appName)
	if err != nil {
		http.Error(w, `{"status":"-3","error":"`+err.Error()+`"}`, 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *AuthHandler) SaveDeviceToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Retrieve token (Authorization + fallback "?token=")
	auth := r.Header.Get("Authorization")
	var token string

	if strings.HasPrefix(auth, "Bearer ") {
		token = strings.TrimPrefix(auth, "Bearer ")
	}

	if token == "" {
		// temporary backward compatibility
		token = r.URL.Query().Get("token")
	}

	if token == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}

	var req SaveDeviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"status":"-2","error":"invalid payload"}`, 400)
		return
	}

	resp, err := h.svc.SaveDeviceToken(ctx, token, req.DeviceToken, req.DeviceID, req.App)
	if err != nil {
		http.Error(w, `{"status":"-3","error":"`+err.Error()+`"}`, 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
