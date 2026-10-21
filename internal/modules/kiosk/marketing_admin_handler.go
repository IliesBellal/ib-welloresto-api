package kiosk

// Routes back-office du ruban marketing : /pos/settings/kiosk/marketing/media.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

const (
	maxMarketingImageBytes = 5 << 20
	maxMarketingVideoBytes = 50 << 20

	// marketingMultipartOverheadBytes couvre les en-têtes multipart autour du
	// fichier : la limite de lecture du corps vaut le plus gros fichier
	// autorisé plus cette marge.
	marketingMultipartOverheadBytes = 1 << 20

	// marketingMultipartMemoryBytes : au-delà, Go déborde la partie sur un
	// fichier temporaire. Volontairement bien inférieur au plafond vidéo : une
	// vidéo de 50 Mo ne doit pas tenir en mémoire le temps de son envoi vers R2.
	marketingMultipartMemoryBytes = 8 << 20

	// marketingJSONBodyBytes borne les corps JSON (durée, ordre) : ils tiennent
	// en quelques centaines d'octets.
	marketingJSONBodyBytes = 64 << 10

	// sniffBytes : de quoi reconnaître tous les formats acceptés.
	sniffBytes = 512
)

// sniffMarketingMime reconnaît le format réel d'un fichier à ses premiers
// octets et retourne son type MIME ("" si non accepté).
//
// Le Content-Type de la partie multipart est déclaré par le client : un fichier
// d'un autre format renommé en .mp4 passerait la validation par type déclaré, et
// une borne mettrait ensuite la rotation en échec sans que personne ne comprenne
// pourquoi. http.DetectContentType couvre les images ; les deux conteneurs
// vidéo sont reconnus à la main, sa détection MP4 exigeant une marque « mp4… »
// que des fichiers valides (marque majeure « isom » ou « M4V ») n'ont pas
// toujours.
func sniffMarketingMime(head []byte) string {
	switch {
	case len(head) >= 12 && string(head[4:8]) == "ftyp" && isMP4Brand(string(head[8:12])):
		return "video/mp4"
	case len(head) >= 4 && bytes.Equal(head[:4], []byte{0x1A, 0x45, 0xDF, 0xA3}):
		return "video/webm"
	}

	detected := http.DetectContentType(head)
	if idx := strings.Index(detected, ";"); idx != -1 {
		detected = detected[:idx]
	}
	if r2.ValidateImageType(detected) {
		return detected
	}
	return ""
}

// isMP4Brand dit si la marque majeure d'une boîte « ftyp » est celle d'une
// vidéo MP4. La boîte « ftyp » ne suffit pas : HEIC, AVIF et QuickTime
// l'utilisent aussi (marques « heic », « avif », « mif1 », « qt  »...), et une
// photo d'iPhone prise pour une vidéo ferait échouer le lecteur de la borne.
// Liste blanche des marques courantes des encodeurs MP4 (ffmpeg, HandBrake,
// exports de montage).
func isMP4Brand(brand string) bool {
	switch {
	case strings.HasPrefix(brand, "mp4"), strings.HasPrefix(brand, "iso"):
		return true
	}
	switch brand {
	case "M4V ", "avc1", "dash", "MSNV", "f4v ":
		return true
	}
	return false
}

// ListMarketingMedia — GET /pos/settings/kiosk/marketing/media.
func (h *AdminHandler) ListMarketingMedia(w http.ResponseWriter, r *http.Request) {
	const fnName = "list_marketing_media"

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", fnName, models.ErrUnauthorized)
		return
	}

	resp, err := h.service.ListMarketingMedia(r.Context(), user.MerchantID)
	if err != nil {
		models.SendErrorJSON(w, "kiosk", fnName, err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", fnName, resp)
}

// CreateMarketingMedia — POST /pos/settings/kiosk/marketing/media
// (multipart/form-data, champ `file` + `duration_seconds` facultatif).
//
// Le kind (image/vidéo) est déduit du contenu du fichier, jamais du formulaire.
// L'identifiant est généré AVANT l'upload : c'est lui qui nomme l'objet R2.
func (h *AdminHandler) CreateMarketingMedia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)
	const fnName = "create_marketing_media"

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", fnName, models.ErrUnauthorized)
		return
	}

	// Rotation pleine : on refuse avant de lire un fichier de 50 Mo.
	if err := h.service.CheckMarketingCapacity(ctx, user.MerchantID); err != nil {
		models.SendErrorJSON(w, "kiosk", fnName, err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxMarketingVideoBytes+marketingMultipartOverheadBytes)
	if err := r.ParseMultipartForm(marketingMultipartMemoryBytes); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "kiosk", fnName, map[string]string{"error": "file_too_large_or_invalid"})
		return
	}
	// Efface les parties débordées sur disque, y compris sur les chemins
	// d'erreur ci-dessous.
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		models.SendJSON(w, http.StatusBadRequest, "kiosk", fnName, map[string]string{"error": "missing_file_field"})
		return
	}
	defer file.Close()

	head := make([]byte, sniffBytes)
	n, readErr := io.ReadFull(file, head)
	if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
		models.SendJSON(w, http.StatusBadRequest, "kiosk", fnName, map[string]string{"error": "file_unreadable"})
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		log.Error("kiosk admin: rewind marketing media failed", zap.Error(err))
		models.SendJSON(w, http.StatusBadRequest, "kiosk", fnName, map[string]string{"error": "file_unreadable"})
		return
	}

	contentType := sniffMarketingMime(head[:n])
	var kind, ext string
	switch {
	case r2.ValidateImageType(contentType):
		kind = marketingKindImage
		ext = r2.GetExtensionFromContentType(contentType)
		if header.Size > maxMarketingImageBytes {
			models.SendJSON(w, http.StatusBadRequest, "kiosk", fnName, map[string]string{"error": "file_too_large"})
			return
		}
	case r2.ValidateVideoType(contentType):
		kind = marketingKindVideo
		ext = r2.GetVideoExtensionFromContentType(contentType)
		if header.Size > maxMarketingVideoBytes {
			models.SendJSON(w, http.StatusBadRequest, "kiosk", fnName, map[string]string{"error": "file_too_large"})
			return
		}
	default:
		models.SendJSON(w, http.StatusBadRequest, "kiosk", fnName, map[string]string{
			"error":   "invalid_media_type",
			"message": "Only JPEG, PNG, WebP images and MP4, WebM videos are allowed",
		})
		return
	}

	// Facultative : un champ absent laisse le média suivre la durée par défaut.
	// Un champ présent mais illisible est refusé plutôt qu'ignoré, pour ne pas
	// laisser croire à une durée appliquée.
	var durationSeconds *int
	if raw := r.FormValue("duration_seconds"); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil {
			models.SendErrorJSON(w, "kiosk", fnName, models.ErrKioskMediaInvalid)
			return
		}
		durationSeconds = &parsed
	}

	mediaID := NewMarketingMediaID()
	key := r2.GenerateKioskMarketingMediaKey(user.MerchantID, mediaID, ext)

	publicURL, err := h.r2Client.UploadFile(ctx, key, file, contentType)
	if err != nil {
		log.Error("kiosk admin: upload marketing media failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", fnName, models.ErrKioskMediaInvalid)
		return
	}

	resp, err := h.service.CreateMarketingMedia(ctx, user.MerchantID, mediaID, kind, publicURL, durationSeconds)
	if err != nil {
		// L'insertion a échoué après l'upload : effacer l'objet, sinon il reste
		// orphelin dans le bucket sans aucune ligne pour le référencer.
		if delErr := h.r2Client.DeleteFile(ctx, key); delErr != nil {
			log.Warn("kiosk admin: cleanup orphan marketing media failed", zap.Error(delErr), zap.String("key", key))
		}
		models.SendErrorJSON(w, "kiosk", fnName, err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", fnName, resp)
}

// UpdateMarketingMedia — PUT /pos/settings/kiosk/marketing/media/{media_id}.
// Règle la durée propre d'un média ; `{"duration_seconds": null}` la retire.
func (h *AdminHandler) UpdateMarketingMedia(w http.ResponseWriter, r *http.Request) {
	const fnName = "update_marketing_media"

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", fnName, models.ErrUnauthorized)
		return
	}

	var req UpdateMarketingMediaRequest
	r.Body = http.MaxBytesReader(w, r.Body, marketingJSONBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "kiosk", fnName, models.ErrInvalidRequestBody)
		return
	}

	resp, err := h.service.UpdateMarketingMedia(r.Context(), user.MerchantID, chi.URLParam(r, "media_id"), req)
	if err != nil {
		models.SendErrorJSON(w, "kiosk", fnName, err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", fnName, resp)
}

// ReorderMarketingMedia — PUT /pos/settings/kiosk/marketing/media/reorder.
// Endpoint dédié plutôt qu'un sort_order passé à chaque PUT : l'ordre est une
// propriété de la liste, pas de chaque élément.
func (h *AdminHandler) ReorderMarketingMedia(w http.ResponseWriter, r *http.Request) {
	const fnName = "reorder_marketing_media"

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", fnName, models.ErrUnauthorized)
		return
	}

	var req ReorderMarketingMediaRequest
	r.Body = http.MaxBytesReader(w, r.Body, marketingJSONBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "kiosk", fnName, models.ErrInvalidRequestBody)
		return
	}

	if err := h.service.ReorderMarketingMedia(r.Context(), user.MerchantID, req.OrderedIDs); err != nil {
		models.SendErrorJSON(w, "kiosk", fnName, err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", fnName, map[string]string{"status": "reordered"})
}

// ResetMarketingMediaDurations — POST /pos/settings/kiosk/marketing/media/reset-durations.
// Fait suivre la durée par défaut à tous les médias.
func (h *AdminHandler) ResetMarketingMediaDurations(w http.ResponseWriter, r *http.Request) {
	const fnName = "reset_marketing_media_durations"

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", fnName, models.ErrUnauthorized)
		return
	}

	reset, err := h.service.ResetMarketingMediaDurations(r.Context(), user.MerchantID)
	if err != nil {
		models.SendErrorJSON(w, "kiosk", fnName, err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", fnName, map[string]int64{"reset": reset})
}

// DeleteMarketingMedia — DELETE /pos/settings/kiosk/marketing/media/{media_id}.
func (h *AdminHandler) DeleteMarketingMedia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)
	const fnName = "delete_marketing_media"

	user := middleware.GetUser(r)
	if user == nil {
		models.SendErrorJSON(w, "kiosk", fnName, models.ErrUnauthorized)
		return
	}

	deleted, err := h.service.DeleteMarketingMedia(ctx, user.MerchantID, chi.URLParam(r, "media_id"))
	if err != nil {
		models.SendErrorJSON(w, "kiosk", fnName, err)
		return
	}

	// Nettoyage R2 best-effort, APRÈS la suppression en base : un fichier
	// orphelin est un désagrément, une ligne qui pointe vers un fichier absent
	// ferait échouer le téléchargement sur chaque borne.
	if key := h.r2Client.GetKeyFromURL(deleted.URL); key != "" {
		if err := h.r2Client.DeleteFile(ctx, key); err != nil {
			log.Warn("kiosk admin: delete marketing media from r2 failed", zap.Error(err), zap.String("key", key))
		}
	}

	models.SendJSON(w, http.StatusOK, "kiosk", fnName, map[string]string{"status": "deleted"})
}
