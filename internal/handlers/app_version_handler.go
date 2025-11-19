package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/services"
)

type AppVersionHandler struct {
	service *services.AppVersionService
}

func NewAppVersionHandler(s *services.AppVersionService) *AppVersionHandler {
	return &AppVersionHandler{service: s}
}

type CheckAppVersionRequest struct {
	Version string `json:"version"`
	App     string `json:"app"`
}

func (h *AppVersionHandler) CheckAppVersion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get token (header or ?token= fallback)
	token := middleware.GetToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}

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

	resp, err := h.service.CheckAppVersion(ctx, token, version, appName)
	if err != nil {
		http.Error(w, `{"status":"-3","error":"`+err.Error()+`"}`, 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
