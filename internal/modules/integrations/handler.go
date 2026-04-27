package integrations

import (
	"encoding/json"
	"fmt"
	"net/http"
	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

// maxImageSize is the max multipart body size accepted for branding image uploads (5 MB).
const maxImageSize = 5 << 20

type Handler struct {
	svc      *Service
	r2Client *r2.Client
}

func NewHandler(svc *Service, r2Client *r2.Client) *Handler {
	return &Handler{svc: svc, r2Client: r2Client}
}

// GetUberEats handles GET /integrations/uber-eats
func (h *Handler) GetUberEats(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	integration, err := h.svc.GetUberEats(r.Context(), user.MerchantID)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "integrations", "get_uber_eats", map[string]string{"error": err.Error()})
		return
	}
	if integration == nil {
		models.SendJSON(w, http.StatusNotFound, "integrations", "get_uber_eats", map[string]string{"error": "not_configured"})
		return
	}

	models.SendJSON(w, http.StatusOK, "integrations", "get_uber_eats", IntegrationData{Integration: integration})
}

// GetDeliveroo handles GET /integrations/deliveroo
func (h *Handler) GetDeliveroo(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	integration, err := h.svc.GetDeliveroo(r.Context(), user.MerchantID)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "integrations", "get_deliveroo", map[string]string{"error": err.Error()})
		return
	}
	if integration == nil {
		models.SendJSON(w, http.StatusNotFound, "integrations", "get_deliveroo", map[string]string{"error": "not_configured"})
		return
	}

	models.SendJSON(w, http.StatusOK, "integrations", "get_deliveroo", IntegrationData{Integration: integration})
}

// GetScanNOrder handles GET /integrations/scannorder
func (h *Handler) GetScanNOrder(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	integration, err := h.svc.GetScanNOrder(r.Context(), user.MerchantID)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "integrations", "get_scannorder", map[string]string{"error": err.Error()})
		return
	}
	if integration == nil {
		models.SendJSON(w, http.StatusNotFound, "integrations", "get_scannorder", map[string]string{"error": "not_configured"})
		return
	}

	models.SendJSON(w, http.StatusOK, "integrations", "get_scannorder", IntegrationData{Integration: integration})
}

// UploadScanNOrderLogo handles POST /integrations/scannorder/logo
func (h *Handler) UploadScanNOrderLogo(w http.ResponseWriter, r *http.Request) {
	h.uploadScanNOrderImage(w, r, "logo", "logo_url")
}

// UploadScanNOrderBanner handles POST /integrations/scannorder/banner
func (h *Handler) UploadScanNOrderBanner(w http.ResponseWriter, r *http.Request) {
	h.uploadScanNOrderImage(w, r, "banner", "banner_url")
}

// uploadScanNOrderImage is the shared implementation for logo/banner uploads.
// imageType is "logo" or "banner"; dbColumn is "logo_url" or "banner_url".
func (h *Handler) uploadScanNOrderImage(w http.ResponseWriter, r *http.Request, imageType, dbColumn string) {
	ctx := r.Context()
	log := logger.FromContext(ctx)
	fnName := "upload_scannorder_" + imageType

	user := middleware.GetUser(r)

	// 1. Parse multipart form (5 MB limit)
	if err := r.ParseMultipartForm(maxImageSize); err != nil {
		log.Error(fmt.Sprintf("[ERROR] %s ParseMultipartForm: %s", fnName, err.Error()))
		models.SendJSON(w, http.StatusBadRequest, "integrations", fnName, map[string]string{"error": "file_too_large_or_invalid"})
		return
	}

	// 2. Retrieve file
	file, header, err := r.FormFile("photo")
	if err != nil {
		log.Error(fmt.Sprintf("[ERROR] %s FormFile: %s", fnName, err.Error()))
		models.SendJSON(w, http.StatusBadRequest, "integrations", fnName, map[string]string{"error": "missing_photo_field"})
		return
	}
	defer file.Close()

	// 3. Validate MIME type
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = r2.GetContentTypeFromExtension(header.Filename)
	}
	if !r2.ValidateImageType(contentType) {
		models.SendJSON(w, http.StatusBadRequest, "integrations", fnName, map[string]string{
			"error":   "invalid_image_type",
			"message": "Only JPEG, PNG, and WebP images are allowed",
		})
		return
	}

	// 4. Delete old image from R2 (non-blocking)
	if oldURL, err := h.svc.GetScanNOrderCurrentImageURL(ctx, user.MerchantID, dbColumn); err == nil && oldURL != "" {
		if oldKey := h.r2Client.GetKeyFromURL(oldURL); oldKey != "" {
			if err := h.r2Client.DeleteFile(ctx, oldKey); err != nil {
				log.Warn(fmt.Sprintf("[WARN] %s DeleteFile (old image): %s", fnName, err.Error()))
			}
		}
	}

	// 5. Build R2 key and upload
	ext := r2.GetExtensionFromContentType(contentType)
	key := r2.GenerateScanNOrderKey(user.MerchantID, imageType, ext)

	publicURL, err := h.r2Client.UploadFile(ctx, key, file, contentType)
	if err != nil {
		log.Error(fmt.Sprintf("[ERROR] %s UploadFile: %s", fnName, err.Error()))
		models.SendErrorJSON(w, "integrations", fnName, fmt.Errorf("failed to upload image"))
		return
	}

	// 6. Persist URL in DB
	if err := h.svc.UpdateScanNOrderImageURL(ctx, user.MerchantID, dbColumn, publicURL); err != nil {
		log.Error(fmt.Sprintf("[ERROR] %s UpdateScanNOrderImageURL: %s", fnName, err.Error()))
		models.SendErrorJSON(w, "integrations", fnName, err)
		return
	}

	models.SendJSON(w, http.StatusOK, "integrations", fnName, map[string]interface{}{
		"status":    "success",
		"photo_url": publicURL,
	})
}

// UpdateUberEats handles PATCH /integrations/uber-eats
func (h *Handler) UpdateUberEats(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	var req UpdateIntegrationRequest
	if err := decodeJSON(r, &req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "integrations", "update_uber_eats", map[string]string{"error": "invalid_body"})
		return
	}

	if err := h.svc.UpdateUberEatsSettings(r.Context(), user.MerchantID, req.CommissionRate, req.AutoAcceptOrders); err != nil {
		models.SendErrorJSON(w, "integrations", "update_uber_eats", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "integrations", "update_uber_eats", map[string]string{"status": "success"})
}

// DisableUberEats handles PATCH /integrations/uber-eats/disable
func (h *Handler) DisableUberEats(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	if err := h.svc.DisableUberEats(r.Context(), user.MerchantID); err != nil {
		models.SendErrorJSON(w, "integrations", "disable_uber_eats", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "integrations", "disable_uber_eats", map[string]string{"status": "success"})
}

// UpdateDeliveroo handles PATCH /integrations/deliveroo
func (h *Handler) UpdateDeliveroo(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	var req UpdateIntegrationRequest
	if err := decodeJSON(r, &req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "integrations", "update_deliveroo", map[string]string{"error": "invalid_body"})
		return
	}

	if err := h.svc.UpdateDeliverooSettings(r.Context(), user.MerchantID, req.CommissionRate, req.AutoAcceptOrders); err != nil {
		models.SendErrorJSON(w, "integrations", "update_deliveroo", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "integrations", "update_deliveroo", map[string]string{"status": "success"})
}

// DisableDeliveroo handles PATCH /integrations/deliveroo/disable
func (h *Handler) DisableDeliveroo(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	if err := h.svc.DisableDeliveroo(r.Context(), user.MerchantID); err != nil {
		models.SendErrorJSON(w, "integrations", "disable_deliveroo", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "integrations", "disable_deliveroo", map[string]string{"status": "success"})
}

// decodeJSON is a small helper to decode a JSON request body.
func decodeJSON(r *http.Request, dst interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dst)
}
