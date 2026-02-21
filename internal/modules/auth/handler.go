package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
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
		errorData := map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		}
		models.SendJSON(w, "auth", "login_error", errorData)
		return
	}

	models.SendJSON(w, "auth", "login", resp)
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
		w.WriteHeader(500)
		models.SendJSON(w, "app", "version_check_error", map[string]string{"status": "-3", "error": err.Error()})
		return
	}

	models.SendJSON(w, "app", "version_check", resp)
}

func (h *AuthHandler) SaveDeviceToken(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}
	ctx := r.Context()

	var req SaveDeviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"status":"-2","error":"invalid payload"}`, 400)
		return
	}

	resp, err := h.svc.SaveDeviceToken(ctx, token, req.DeviceToken, req.DeviceID, req.App)
	if err != nil {
		w.WriteHeader(500)
		models.SendJSON(w, "device", "save_token_error", map[string]string{"status": "-3", "error": err.Error()})
		return
	}

	models.SendJSON(w, "device", "save_token", resp)
}
