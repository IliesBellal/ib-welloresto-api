package kiosk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"welloresto-api/internal/infrastructure/r2"
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
	service  *Service
	r2Client *r2.Client
}

func NewAdminHandler(s *Service, r2Client *r2.Client) *AdminHandler {
	return &AdminHandler{service: s, r2Client: r2Client}
}

// Plafonds d'upload par type d'asset Kiosk : logo et image de veille n'ont
// pas la même taille max (2 Mo vs 5 Mo) — corrigé en incrément 4, les deux
// partageaient auparavant maxKioskSettingsImageBytes = 2 Mo, voir
// docs/KIOSK_DECISIONS.md.
const (
	maxKioskLogoBytes      = 2 << 20
	maxKioskIdleImageBytes = 5 << 20
	maxKioskIdleVideoBytes = 50 << 20
)

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

func (h *AdminHandler) GetKioskDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", "get_kiosk_device", models.ErrUnauthorized)
		return
	}

	deviceID := chi.URLParam(r, "device_id")
	if deviceID == "" {
		models.SendErrorJSON(w, "kiosk", "get_kiosk_device", models.ErrMissingResourceID)
		return
	}

	resp, err := h.service.GetKioskDevice(ctx, user.MerchantID, deviceID)
	if err != nil {
		log.Warn("kiosk admin: get device failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "get_kiosk_device", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "get_kiosk_device", resp)
}

func (h *AdminHandler) UpdateKioskDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", "update_kiosk_device", models.ErrUnauthorized)
		return
	}

	deviceID := chi.URLParam(r, "device_id")
	if deviceID == "" {
		models.SendErrorJSON(w, "kiosk", "update_kiosk_device", models.ErrMissingResourceID)
		return
	}

	var req UpdateKioskDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "kiosk", "update_kiosk_device", models.ErrInvalidRequestBody)
		return
	}

	resp, err := h.service.UpdateKioskDeviceName(ctx, user.MerchantID, deviceID, req.Name)
	if err != nil {
		log.Warn("kiosk admin: update device failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "update_kiosk_device", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "update_kiosk_device", resp)
}

func (h *AdminHandler) EnableKioskDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", "enable_kiosk_device", models.ErrUnauthorized)
		return
	}

	deviceID := chi.URLParam(r, "device_id")
	if deviceID == "" {
		models.SendErrorJSON(w, "kiosk", "enable_kiosk_device", models.ErrMissingResourceID)
		return
	}

	resp, err := h.service.EnableKioskDevice(ctx, user.MerchantID, deviceID)
	if err != nil {
		log.Warn("kiosk admin: enable device failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "enable_kiosk_device", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "enable_kiosk_device", resp)
}

func (h *AdminHandler) DisableKioskDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", "disable_kiosk_device", models.ErrUnauthorized)
		return
	}

	deviceID := chi.URLParam(r, "device_id")
	if deviceID == "" {
		models.SendErrorJSON(w, "kiosk", "disable_kiosk_device", models.ErrMissingResourceID)
		return
	}

	resp, err := h.service.DisableKioskDevice(ctx, user.MerchantID, deviceID)
	if err != nil {
		log.Warn("kiosk admin: disable device failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "disable_kiosk_device", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "disable_kiosk_device", resp)
}

// GetAdminPin handles GET /pos/settings/kiosk/devices/{device_id}/admin-pin
// — consultation du PIN admin courant depuis le POS (déchiffré à la volée,
// jamais stocké en clair). 404 dédié si la borne n'a pas encore de PIN
// chiffré en base (borne créée avant cette fonctionnalité) : le message
// invite à régénérer plutôt que de laisser un 500 ambigu.
func (h *AdminHandler) GetAdminPin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", "get_admin_pin", models.ErrUnauthorized)
		return
	}

	deviceID := chi.URLParam(r, "device_id")
	if deviceID == "" {
		models.SendErrorJSON(w, "kiosk", "get_admin_pin", models.ErrMissingResourceID)
		return
	}

	resp, err := h.service.GetAdminPin(ctx, user.MerchantID, deviceID)
	if err != nil {
		log.Warn("kiosk admin: get admin pin failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "get_admin_pin", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "get_admin_pin", resp)
}

// RegenerateAdminPin handles POST /pos/settings/kiosk/devices/{device_id}/regenerate-admin-pin
// — utile si le technicien a perdu le PIN admin reçu une seule fois à
// l'enrôlement. Le nouveau PIN n'est lui aussi retourné qu'une seule fois.
func (h *AdminHandler) RegenerateAdminPin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", "regenerate_admin_pin", models.ErrUnauthorized)
		return
	}

	deviceID := chi.URLParam(r, "device_id")
	if deviceID == "" {
		models.SendErrorJSON(w, "kiosk", "regenerate_admin_pin", models.ErrMissingResourceID)
		return
	}

	resp, err := h.service.RegenerateAdminPin(ctx, user.MerchantID, deviceID)
	if err != nil {
		log.Warn("kiosk admin: regenerate admin pin failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "regenerate_admin_pin", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "regenerate_admin_pin", resp)
}

// SetKioskStatusFromPOS handles POST /pos/kiosk/{kiosk_id}/status — activation
// /désactivation d'une borne depuis l'app POS Flutter (staff en salle), pas
// depuis le back-office web (voir EnableKioskDevice/DisableKioskDevice).
// triggered_by = "pos" dans l'event kiosk_status_changed diffusé.
func (h *AdminHandler) SetKioskStatusFromPOS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", "set_kiosk_status_from_pos", models.ErrUnauthorized)
		return
	}

	kioskID := chi.URLParam(r, "kiosk_id")
	if kioskID == "" {
		models.SendErrorJSON(w, "kiosk", "set_kiosk_status_from_pos", models.ErrMissingResourceID)
		return
	}

	var req SetKioskStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "kiosk", "set_kiosk_status_from_pos", models.ErrInvalidRequestBody)
		return
	}

	resp, err := h.service.SetKioskStatusFromPOS(ctx, user.MerchantID, kioskID, req.Enabled)
	if err != nil {
		log.Warn("kiosk pos: set status failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "set_kiosk_status_from_pos", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "set_kiosk_status_from_pos", resp)
}

func (h *AdminHandler) ListEnrollmentCodes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", "list_enrollment_codes", models.ErrUnauthorized)
		return
	}

	resp, err := h.service.ListEnrollmentCodes(ctx, user.MerchantID)
	if err != nil {
		log.Warn("kiosk admin: list enrollment codes failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "list_enrollment_codes", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "list_enrollment_codes", resp)
}

func (h *AdminHandler) DeleteEnrollmentCode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", "delete_enrollment_code", models.ErrUnauthorized)
		return
	}

	codeID := chi.URLParam(r, "code_id")
	if codeID == "" {
		models.SendErrorJSON(w, "kiosk", "delete_enrollment_code", models.ErrMissingResourceID)
		return
	}

	if err := h.service.RevokeEnrollmentCode(ctx, user.MerchantID, codeID); err != nil {
		log.Warn("kiosk admin: delete enrollment code failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "delete_enrollment_code", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "delete_enrollment_code", map[string]string{"status": "revoked"})
}

func (h *AdminHandler) UploadKioskLogo(w http.ResponseWriter, r *http.Request) {
	h.uploadSettingsImage(w, r, "logo", maxKioskLogoBytes)
}

func (h *AdminHandler) UploadKioskIdleImage(w http.ResponseWriter, r *http.Request) {
	h.uploadSettingsImage(w, r, "idle", maxKioskIdleImageBytes)
}

// uploadSettingsImage factorise l'upload logo/idle-image : même validation,
// même nettoyage de l'ancien fichier R2, seule la clé, le plafond et le champ
// persisté changent. imageType vaut "logo" ou "idle".
func (h *AdminHandler) uploadSettingsImage(w http.ResponseWriter, r *http.Request, imageType string, maxBytes int64) {
	ctx := r.Context()
	log := logger.FromContext(ctx)
	fnName := "upload_kiosk_" + imageType

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", fnName, models.ErrUnauthorized)
		return
	}

	if err := r.ParseMultipartForm(maxBytes); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "kiosk", fnName, map[string]string{"error": "file_too_large_or_invalid"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		models.SendJSON(w, http.StatusBadRequest, "kiosk", fnName, map[string]string{"error": "missing_file_field"})
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = r2.GetContentTypeFromExtension(header.Filename)
	}
	if !r2.ValidateImageType(contentType) {
		models.SendJSON(w, http.StatusBadRequest, "kiosk", fnName, map[string]string{
			"error":   "invalid_image_type",
			"message": "Only JPEG, PNG, and WebP images are allowed",
		})
		return
	}

	settings, err := h.service.GetSettings(ctx, user.MerchantID)
	if err != nil {
		log.Warn("kiosk admin: get settings before upload failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", fnName, err)
		return
	}
	var oldURL string
	if imageType == "logo" && settings.LogoURL != nil {
		oldURL = *settings.LogoURL
	} else if imageType == "idle" && settings.IdleImageURL != nil {
		oldURL = *settings.IdleImageURL
	}

	ext := r2.GetExtensionFromContentType(contentType)
	key := r2.GenerateKioskKey(user.MerchantID, imageType, ext)

	if oldURL != "" {
		if oldKey := h.r2Client.GetKeyFromURL(oldURL); oldKey != "" && oldKey != key {
			if err := h.r2Client.DeleteFile(ctx, oldKey); err != nil {
				log.Warn("kiosk admin: delete old image failed", zap.Error(err))
			}
		}
	}

	publicURL, err := h.r2Client.UploadFile(ctx, key, file, contentType)
	if err != nil {
		log.Error("kiosk admin: upload image failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", fnName, fmt.Errorf("failed to upload image"))
		return
	}

	var updated *KioskSettingsResponse
	if imageType == "logo" {
		updated, err = h.service.SetLogoURL(ctx, user.MerchantID, publicURL)
	} else {
		updated, err = h.service.SetIdleImageURL(ctx, user.MerchantID, publicURL)
	}
	if err != nil {
		log.Error("kiosk admin: persist image url failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", fnName, err)
		return
	}

	if imageType == "logo" {
		models.SendJSON(w, http.StatusOK, "kiosk", fnName, map[string]string{"logo_url": *updated.LogoURL})
	} else {
		models.SendJSON(w, http.StatusOK, "kiosk", fnName, map[string]string{"idle_image_url": *updated.IdleImageURL})
	}
}

// UploadKioskIdleVideo uploade la vidéo de veille affichée entre deux
// clients sur la borne. Même séquence que uploadSettingsImage (ancien
// fichier supprimé en best-effort, clé déterministe, upsert kiosk_settings)
// mais types/plafond vidéo dédiés — pas factorisé avec uploadSettingsImage
// car la validation de type (image vs vidéo) et l'extension diffèrent.
func (h *AdminHandler) UploadKioskIdleVideo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)
	const fnName = "upload_kiosk_idle_video"

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", fnName, models.ErrUnauthorized)
		return
	}

	if err := r.ParseMultipartForm(maxKioskIdleVideoBytes); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "kiosk", fnName, map[string]string{"error": "file_too_large_or_invalid"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		models.SendJSON(w, http.StatusBadRequest, "kiosk", fnName, map[string]string{"error": "missing_file_field"})
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		switch strings.ToLower(filepath.Ext(header.Filename)) {
		case ".mp4":
			contentType = "video/mp4"
		case ".webm":
			contentType = "video/webm"
		}
	}
	if !r2.ValidateVideoType(contentType) {
		models.SendJSON(w, http.StatusBadRequest, "kiosk", fnName, map[string]string{
			"error":   "invalid_video_type",
			"message": "Only MP4 and WebM videos are allowed",
		})
		return
	}

	settings, err := h.service.GetSettings(ctx, user.MerchantID)
	if err != nil {
		log.Warn("kiosk admin: get settings before upload failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", fnName, err)
		return
	}
	var oldURL string
	if settings.IdleVideoURL != nil {
		oldURL = *settings.IdleVideoURL
	}

	ext := r2.GetVideoExtensionFromContentType(contentType)
	key := r2.GenerateKioskKey(user.MerchantID, "idle_video", ext)

	if oldURL != "" {
		if oldKey := h.r2Client.GetKeyFromURL(oldURL); oldKey != "" && oldKey != key {
			if err := h.r2Client.DeleteFile(ctx, oldKey); err != nil {
				log.Warn("kiosk admin: delete old idle video failed", zap.Error(err))
			}
		}
	}

	publicURL, err := h.r2Client.UploadFile(ctx, key, file, contentType)
	if err != nil {
		log.Error("kiosk admin: upload idle video failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", fnName, fmt.Errorf("failed to upload video"))
		return
	}

	updated, err := h.service.SetIdleVideoURL(ctx, user.MerchantID, publicURL)
	if err != nil {
		log.Error("kiosk admin: persist idle video url failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", fnName, err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", fnName, map[string]string{"idle_video_url": *updated.IdleVideoURL})
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
