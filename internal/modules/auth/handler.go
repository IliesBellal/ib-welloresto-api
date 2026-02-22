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
		models.SendJSON(w, http.StatusBadRequest, "auth", "login", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.svc.Login(r.Context(), req, token)
	if err != nil {
		errorData := map[string]interface{}{
			"error": err.Error(),
		}
		models.SendJSON(w, http.StatusInternalServerError, "auth", "login", errorData)
		return
	}

	models.SendJSON(w, http.StatusOK, "auth", "login", resp)
}

func (h *AuthHandler) CheckAppVersion(w http.ResponseWriter, r *http.Request) {
	// Get token (header or ?token= fallback)
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "auth", "check", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	// Parse JSON body
	var req CheckAppVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "app", "version_check", map[string]string{"error": "invalid_request"})
		return
	}

	version := strings.TrimSpace(req.Version)
	appName := strings.TrimSpace(req.App)
	if version == "" || appName == "" {
		models.SendJSON(w, http.StatusBadRequest, "app", "version_check", map[string]string{"error": "missing_fields"})
		return
	}

	resp, err := h.svc.CheckAppVersion(ctx, token, version, appName)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "app", "version_check", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "app", "version_check", resp)
}

func (h *AuthHandler) SaveDeviceToken(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "device", "save_token", map[string]string{"error": "missing_token"})
		return
	}
	ctx := r.Context()

	var req SaveDeviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "device", "save_token", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.svc.SaveDeviceToken(ctx, token, req.DeviceToken, req.DeviceID, req.App)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "device", "save_token", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "device", "save_token", resp)
}
