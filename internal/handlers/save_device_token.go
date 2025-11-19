package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"welloresto-api/internal/services"
)

type DeviceHandler struct {
	deviceService *services.DeviceService
}

func NewDeviceHandler(s *services.DeviceService) *DeviceHandler {
	return &DeviceHandler{deviceService: s}
}

type SaveDeviceTokenRequest struct {
	DeviceToken string `json:"device_token"`
	DeviceID    string `json:"device_id"`
	App         string `json:"app"`
}

func (h *DeviceHandler) SaveDeviceToken(w http.ResponseWriter, r *http.Request) {
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

	resp, err := h.deviceService.SaveDeviceToken(ctx, token, req.DeviceToken, req.DeviceID, req.App)
	if err != nil {
		http.Error(w, `{"status":"-3","error":"`+err.Error()+`"}`, 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
