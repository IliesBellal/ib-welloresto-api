package kiosk

import (
	"encoding/json"
	"net/http"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// AdminHandler regroupe les routes back-office du module Kiosk
// (/pos/settings/kiosk/...), protégées par authMiddleware (utilisateur
// staff), pas par KioskAuth (device).
type AdminHandler struct {
	service *Service
}

func NewAdminHandler(s *Service) *AdminHandler {
	return &AdminHandler{service: s}
}

func (h *AdminHandler) GenerateEnrollmentCode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", "generate_enrollment_code", models.ErrUnauthorized)
		return
	}

	resp, err := h.service.GenerateEnrollmentCode(ctx, user.MerchantID, user.UserID)
	if err != nil {
		log.Warn("kiosk admin: generate enrollment code failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "generate_enrollment_code", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "generate_enrollment_code", resp)
}

func (h *AdminHandler) ListKioskDevices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", "list_kiosk_devices", models.ErrUnauthorized)
		return
	}

	resp, err := h.service.ListKioskDevices(ctx, user.MerchantID)
	if err != nil {
		log.Warn("kiosk admin: list devices failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "list_kiosk_devices", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "list_kiosk_devices", resp)
}

func (h *AdminHandler) RevokeKioskDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", "revoke_kiosk_device", models.ErrUnauthorized)
		return
	}

	deviceID := chi.URLParam(r, "device_id")
	if deviceID == "" {
		models.SendErrorJSON(w, "kiosk", "revoke_kiosk_device", models.ErrMissingResourceID)
		return
	}

	if err := h.service.RevokeKiosk(ctx, user.MerchantID, deviceID); err != nil {
		log.Warn("kiosk admin: revoke device failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "revoke_kiosk_device", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "revoke_kiosk_device", map[string]string{"status": "revoked"})
}

func (h *AdminHandler) GetKioskSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", "get_kiosk_settings", models.ErrUnauthorized)
		return
	}

	resp, err := h.service.GetSettings(ctx, user.MerchantID)
	if err != nil {
		log.Warn("kiosk admin: get settings failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "get_kiosk_settings", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "get_kiosk_settings", resp)
}

func (h *AdminHandler) UpdateKioskSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", "update_kiosk_settings", models.ErrUnauthorized)
		return
	}

	var req UpdateKioskSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "kiosk", "update_kiosk_settings", models.ErrInvalidRequestBody)
		return
	}

	resp, err := h.service.UpdateSettings(ctx, user.MerchantID, req)
	if err != nil {
		log.Warn("kiosk admin: update settings failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "update_kiosk_settings", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "update_kiosk_settings", resp)
}
