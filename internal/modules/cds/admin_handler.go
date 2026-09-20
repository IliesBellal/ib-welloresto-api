package cds

// Handlers back-office du module CDS (routes /pos/settings/cds/*, auth
// humaine + permission cds.manage). Le parcours device vit dans handler.go.

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// Plafonds d'upload de la zone marketing, alignes sur ceux du module kiosk
// (maxKioskIdleImageBytes / maxKioskIdleVideoBytes).
const (
	maxCDSMediaImageBytes = 5 << 20
	maxCDSMediaVideoBytes = 50 << 20
)

type AdminHandler struct {
	service  *Service
	r2Client *r2.Client
}

func NewAdminHandler(s *Service, r2Client *r2.Client) *AdminHandler {
	return &AdminHandler{service: s, r2Client: r2Client}
}

// ---- Codes d'enrôlement ----

// GenerateEnrollmentCode — POST /pos/settings/cds/enrollment-codes.
// Le code en clair n'est renvoyé qu'ici, une seule fois.
func (h *AdminHandler) GenerateEnrollmentCode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "cds", "generate_enrollment_code", models.ErrUnauthorized)
		return
	}

	resp, err := h.service.GenerateEnrollmentCode(ctx, user.MerchantID, user.UserID)
	if err != nil {
		log.Warn("cds generate enrollment code failed", zap.Error(err))
		models.SendErrorJSON(w, "cds", "generate_enrollment_code", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", "generate_enrollment_code", resp)
}

func (h *AdminHandler) ListEnrollmentCodes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "cds", "list_enrollment_codes", models.ErrUnauthorized)
		return
	}

	resp, err := h.service.ListEnrollmentCodes(ctx, user.MerchantID)
	if err != nil {
		models.SendErrorJSON(w, "cds", "list_enrollment_codes", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", "list_enrollment_codes", resp)
}

func (h *AdminHandler) DeleteEnrollmentCode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "cds", "delete_enrollment_code", models.ErrUnauthorized)
		return
	}

	if err := h.service.DeleteEnrollmentCode(ctx, user.MerchantID, chi.URLParam(r, "code_id")); err != nil {
		models.SendErrorJSON(w, "cds", "delete_enrollment_code", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", "delete_enrollment_code", map[string]string{"status": "deleted"})
}

// ---- Parc d'écrans ----

func (h *AdminHandler) ListDisplays(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "cds", "list_displays", models.ErrUnauthorized)
		return
	}

	resp, err := h.service.ListDisplays(ctx, user.MerchantID)
	if err != nil {
		models.SendErrorJSON(w, "cds", "list_displays", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", "list_displays", resp)
}

func (h *AdminHandler) GetDisplay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "cds", "get_display", models.ErrUnauthorized)
		return
	}

	resp, err := h.service.GetDisplay(ctx, user.MerchantID, chi.URLParam(r, "display_id"))
	if err != nil {
		models.SendErrorJSON(w, "cds", "get_display", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", "get_display", resp)
}

func (h *AdminHandler) UpdateDisplay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "cds", "update_display", models.ErrUnauthorized)
		return
	}

	var req UpdateDisplayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "cds", "update_display", models.ErrInvalidRequestBody)
		return
	}

	if err := h.service.UpdateDisplay(ctx, user.MerchantID, chi.URLParam(r, "display_id"), req); err != nil {
		models.SendErrorJSON(w, "cds", "update_display", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", "update_display", map[string]string{"status": "updated"})
}

// RevokeDisplay — POST /pos/settings/cds/displays/{display_id}/revoke.
// Seule action de cycle de vie : il n'existe ni enable ni disable (D15).
func (h *AdminHandler) RevokeDisplay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "cds", "revoke_display", models.ErrUnauthorized)
		return
	}

	displayID := chi.URLParam(r, "display_id")
	if err := h.service.RevokeDisplay(ctx, user.MerchantID, displayID); err != nil {
		models.SendErrorJSON(w, "cds", "revoke_display", err)
		return
	}

	log.Info("cds display revoked",
		zap.String("merchant_id", user.MerchantID),
		zap.String("display_id", displayID),
	)

	models.SendJSON(w, http.StatusOK, "cds", "revoke_display", map[string]string{"status": "revoked"})
}

func (h *AdminHandler) GetAdminPin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "cds", "get_admin_pin", models.ErrUnauthorized)
		return
	}

	resp, err := h.service.GetAdminPin(ctx, user.MerchantID, chi.URLParam(r, "display_id"))
	if err != nil {
		models.SendErrorJSON(w, "cds", "get_admin_pin", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", "get_admin_pin", resp)
}

// ---- Paramètres ----

func (h *AdminHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "cds", "get_settings", models.ErrUnauthorized)
		return
	}

	resp, err := h.service.GetSettings(ctx, user.MerchantID, chi.URLParam(r, "display_id"))
	if err != nil {
		models.SendErrorJSON(w, "cds", "get_settings", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", "get_settings", resp)
}

func (h *AdminHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "cds", "update_settings", models.ErrUnauthorized)
		return
	}

	var req UpdateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "cds", "update_settings", models.ErrInvalidRequestBody)
		return
	}

	if err := h.service.UpdateSettings(ctx, user.MerchantID, chi.URLParam(r, "display_id"), req); err != nil {
		models.SendErrorJSON(w, "cds", "update_settings", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", "update_settings", map[string]string{"status": "updated"})
}

// ---- Médias marketing ----

func (h *AdminHandler) ListMedia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "cds", "list_media", models.ErrUnauthorized)
		return
	}

	items, err := h.service.ListMedia(ctx, user.MerchantID, chi.URLParam(r, "display_id"))
	if err != nil {
		models.SendErrorJSON(w, "cds", "list_media", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", "list_media", map[string]interface{}{"media": items})
}

// CreateMediaItemRequest — body JSON de POST .../media pour un media sans
// fichier : un QR code, ou une URL deja hebergee ailleurs.
//
// L'ajout d'une image ou d'une video passe par le meme endpoint en
// multipart/form-data (voir CreateMediaItem) : le handler distingue les deux
// sur le Content-Type de la requete.
type CreateMediaItemRequest struct {
	Kind            string  `json:"kind"`
	URL             *string `json:"url"`
	QRPayload       *string `json:"qr_payload"`
	DurationSeconds int     `json:"duration_seconds"`
}

// CreateMediaItem — POST /pos/settings/cds/displays/{display_id}/media.
//
// Deux formes acceptees sur la meme route :
//   - multipart/form-data avec un champ `file` : image ou video, uploadee sur
//     R2 puis enregistree dans la rotation ;
//   - application/json : media sans fichier (QR code, ou URL externe).
//
// Contrairement au visuel de veille du kiosk (cle R2 deterministe, ecrasee a
// chaque upload, d'ou son cache-buster), chaque media CDS a sa propre cle
// derivee de son identifiant : les medias de la rotation coexistent, aucune
// cle n'est jamais reecrite, aucun cache-buster n'est necessaire.
func (h *AdminHandler) CreateMediaItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	const fnName = "create_media"

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "cds", fnName, models.ErrUnauthorized)
		return
	}

	displayID := chi.URLParam(r, "display_id")

	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		h.createMediaItemFromUpload(w, r, user.MerchantID, displayID)
		return
	}

	var req CreateMediaItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "cds", fnName, models.ErrInvalidRequestBody)
		return
	}

	resp, err := h.service.CreateMediaItem(ctx, user.MerchantID, displayID,
		"", req.Kind, req.URL, req.QRPayload, req.DurationSeconds)
	if err != nil {
		models.SendErrorJSON(w, "cds", fnName, err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", fnName, resp)
}

// createMediaItemFromUpload traite la variante multipart : validation du type
// MIME, upload R2, puis insertion.
//
// L'identifiant du media est genere AVANT l'upload : c'est lui qui nomme
// l'objet R2 (r2.GenerateCDSMediaKey), le fichier doit donc etre nomme avant
// que la ligne n'existe en base.
func (h *AdminHandler) createMediaItemFromUpload(w http.ResponseWriter, r *http.Request, merchantID, displayID string) {
	ctx := r.Context()
	log := logger.FromContext(ctx)
	const fnName = "create_media"

	if err := r.ParseMultipartForm(maxCDSMediaVideoBytes); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "cds", fnName, map[string]string{"error": "file_too_large_or_invalid"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		models.SendJSON(w, http.StatusBadRequest, "cds", fnName, map[string]string{"error": "missing_file_field"})
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = contentTypeFromExtension(header.Filename)
	}

	// Le kind n'est pas lu du formulaire mais deduit du type MIME : laisser
	// le client declarer "image" en envoyant une video de 50 Mo mettrait
	// l'ecran en defaut sans que personne ne comprenne pourquoi.
	var kind, ext string
	switch {
	case r2.ValidateImageType(contentType):
		kind = "image"
		ext = r2.GetExtensionFromContentType(contentType)
		if header.Size > maxCDSMediaImageBytes {
			models.SendJSON(w, http.StatusBadRequest, "cds", fnName, map[string]string{"error": "file_too_large"})
			return
		}
	case r2.ValidateVideoType(contentType):
		kind = "video"
		ext = r2.GetVideoExtensionFromContentType(contentType)
	default:
		models.SendJSON(w, http.StatusBadRequest, "cds", fnName, map[string]string{
			"error":   "invalid_media_type",
			"message": "Only JPEG, PNG, WebP images and MP4, WebM videos are allowed",
		})
		return
	}

	durationSeconds := 0
	if raw := r.FormValue("duration_seconds"); raw != "" {
		if parsed, convErr := strconv.Atoi(raw); convErr == nil {
			durationSeconds = parsed
		}
	}

	mediaID := NewMediaItemID()
	key := r2.GenerateCDSMediaKey(merchantID, displayID, mediaID, ext)

	publicURL, err := h.r2Client.UploadFile(ctx, key, file, contentType)
	if err != nil {
		log.Error("cds admin: upload media failed", zap.Error(err))
		models.SendErrorJSON(w, "cds", fnName, models.ErrCDSMediaInvalid)
		return
	}

	resp, err := h.service.CreateMediaItem(ctx, merchantID, displayID, mediaID, kind, &publicURL, nil, durationSeconds)
	if err != nil {
		// L'insertion a echoue apres l'upload : effacer l'objet, sinon il
		// reste orphelin dans le bucket sans aucune ligne pour le referencer.
		if delErr := h.r2Client.DeleteFile(ctx, key); delErr != nil {
			log.Warn("cds admin: cleanup orphan media file failed", zap.Error(delErr), zap.String("key", key))
		}
		models.SendErrorJSON(w, "cds", fnName, err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", fnName, resp)
}

// contentTypeFromExtension sert de repli quand le client n'envoie pas de
// Content-Type sur la partie multipart — meme precaution que
// kiosk.AdminHandler.UploadKioskIdleVideo.
func contentTypeFromExtension(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	default:
		return ""
	}
}

// ReorderMediaRequest — body de PUT .../media/reorder. Endpoint dédié plutôt
// qu'un sort_order passé à chaque PUT : l'ordre est une propriété de la
// liste, pas de chaque élément (même patron que le réordonnancement du module
// menu).
type ReorderMediaRequest struct {
	OrderedIDs []string `json:"ordered_ids"`
}

func (h *AdminHandler) ReorderMedia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "cds", "reorder_media", models.ErrUnauthorized)
		return
	}

	var req ReorderMediaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "cds", "reorder_media", models.ErrInvalidRequestBody)
		return
	}

	if err := h.service.ReorderMedia(ctx, user.MerchantID, chi.URLParam(r, "display_id"), req.OrderedIDs); err != nil {
		models.SendErrorJSON(w, "cds", "reorder_media", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cds", "reorder_media", map[string]string{"status": "reordered"})
}

func (h *AdminHandler) DeleteMediaItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)
	const fnName = "delete_media"

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "cds", fnName, models.ErrUnauthorized)
		return
	}

	deleted, err := h.service.DeleteMediaItem(ctx, user.MerchantID,
		chi.URLParam(r, "display_id"), chi.URLParam(r, "media_id"))
	if err != nil {
		models.SendErrorJSON(w, "cds", fnName, err)
		return
	}

	// Nettoyage R2 best-effort, APRES la suppression en base : un fichier
	// orphelin est un desagrement, une ligne qui pointe vers un fichier
	// disparu est un media casse a l'ecran. L'ordre compte.
	if deleted.URL != nil && *deleted.URL != "" {
		if key := h.r2Client.GetKeyFromURL(*deleted.URL); key != "" {
			if delErr := h.r2Client.DeleteFile(ctx, key); delErr != nil {
				log.Warn("cds admin: delete media file failed", zap.Error(delErr), zap.String("key", key))
			}
		}
	}

	models.SendJSON(w, http.StatusOK, "cds", fnName, map[string]string{"status": "deleted"})
}
