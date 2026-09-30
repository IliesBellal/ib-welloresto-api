package menu

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
)

// Limites d'envoi (Q1 du cadrage). Le back-office normalise chaque photo en
// JPEG de 2576 px au plus avant l'envoi : une photo de plus de 7 Mo n'est
// donc pas passée par lui (et dépasserait 10 Mo en base64 côté API).
const (
	aiFormPhotosField = "photos"
	// aiFormIngredientsField : "true" pour lire aussi les ingrédients des
	// descriptions (case de l'étape photo, décochée par défaut).
	aiFormIngredientsField = "ingredients"
	aiMaxPhotos            = 10
	aiMaxPhotoBytes        = 7 << 20
	aiMaxUploadBytes       = 20 << 20
	aiUploadOverhead       = 1 << 20 // en-têtes multipart
)

// AIImportHandler expose la porte IA de l'import produits. Le commit reste
// POST /menu/import/commit (ImportHandler).
type AIImportHandler struct {
	service *AIImportService
}

func NewAIImportHandler(s *AIImportService) *AIImportHandler {
	return &AIImportHandler{service: s}
}

// StartAIImport — POST /menu/import/ai (multipart, champ « photos » répété,
// champ « ingredients » facultatif).
func (h *AIImportHandler) StartAIImport(w http.ResponseWriter, r *http.Request) {
	const action = "start_ai_import"
	r.Body = http.MaxBytesReader(w, r.Body, aiMaxUploadBytes+aiUploadOverhead)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		aiBadRequest(w, action, "photos_too_large_or_invalid")
		return
	}

	files := r.MultipartForm.File[aiFormPhotosField]
	switch {
	case len(files) == 0:
		aiBadRequest(w, action, "missing_photos")
		return
	case len(files) > aiMaxPhotos:
		aiBadRequest(w, action, "too_many_photos")
		return
	}

	photos := make([][]byte, 0, len(files))
	for _, fh := range files {
		if fh.Size > aiMaxPhotoBytes {
			aiBadRequest(w, action, "photo_too_large")
			return
		}
		f, err := fh.Open()
		if err != nil {
			aiBadRequest(w, action, "photos_too_large_or_invalid")
			return
		}
		data, err := io.ReadAll(io.LimitReader(f, aiMaxPhotoBytes+1))
		_ = f.Close()
		if err != nil || len(data) > aiMaxPhotoBytes {
			aiBadRequest(w, action, "photo_too_large")
			return
		}
		// Type réel, pas l'extension ni l'en-tête déclaré.
		if http.DetectContentType(data) != aiPhotoContentType {
			aiBadRequest(w, action, "photo_not_jpeg")
			return
		}
		photos = append(photos, data)
	}

	withIngredients := r.FormValue(aiFormIngredientsField) == "true"
	resp, err := h.service.StartExtraction(r.Context(), photos, withIngredients)
	if err != nil {
		h.sendError(w, r, action, err)
		return
	}
	models.SendJSON(w, http.StatusAccepted, "menu", action, resp)
}

// GetAIDraft — GET /menu/import/ai/{id}.
func (h *AIImportHandler) GetAIDraft(w http.ResponseWriter, r *http.Request) {
	const action = "get_ai_draft"
	resp, err := h.service.GetDraft(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.sendError(w, r, action, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "menu", action, resp)
}

// RetryAIDraft — POST /menu/import/ai/{id}/retry.
func (h *AIImportHandler) RetryAIDraft(w http.ResponseWriter, r *http.Request) {
	const action = "retry_ai_draft"
	resp, err := h.service.RetryDraft(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.sendError(w, r, action, err)
		return
	}
	models.SendJSON(w, http.StatusAccepted, "menu", action, resp)
}

// ListAIDrafts — GET /menu/import/drafts.
func (h *AIImportHandler) ListAIDrafts(w http.ResponseWriter, r *http.Request) {
	const action = "list_ai_drafts"
	resp, err := h.service.ListDrafts(r.Context())
	if err != nil {
		h.sendError(w, r, action, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "menu", action, resp)
}

// AbandonAIDraft — DELETE /menu/import/drafts/{id}.
func (h *AIImportHandler) AbandonAIDraft(w http.ResponseWriter, r *http.Request) {
	const action = "abandon_ai_draft"
	if err := h.service.AbandonDraft(r.Context(), chi.URLParam(r, "id")); err != nil {
		h.sendError(w, r, action, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "menu", action, map[string]string{"status": AIDraftExpired})
}

// SetMerchantAICredits — PUT /admin/merchants/{id}/menu-ocr-credits (staff
// Wello, RequirePlatformAdmin). Corps : {"credits": n}, nombre total de
// crédits du marchand (n ≥ 0).
func (h *AIImportHandler) SetMerchantAICredits(w http.ResponseWriter, r *http.Request) {
	const action = "set_ai_credits"
	var req SetAICreditsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Credits == nil || *req.Credits < 0 {
		aiBadRequest(w, action, "invalid_credits")
		return
	}
	credits, err := h.service.SetCredits(r.Context(), chi.URLParam(r, "id"), *req.Credits)
	if err != nil {
		h.sendError(w, r, action, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "menu", action, credits)
}

func aiBadRequest(w http.ResponseWriter, action, code string) {
	models.SendJSON(w, http.StatusBadRequest, "menu", action, map[string]string{"error": code})
}

func (h *AIImportHandler) sendError(w http.ResponseWriter, r *http.Request, action string, err error) {
	status, code := http.StatusInternalServerError, "internal_error"
	switch {
	case errors.Is(err, ErrAIImportDisabled):
		status, code = http.StatusServiceUnavailable, ErrAIImportDisabled.Error()
	case errors.Is(err, ErrAICreditsExhausted):
		status, code = http.StatusPaymentRequired, ErrAICreditsExhausted.Error()
	case errors.Is(err, ErrAIDraftAlreadyRunning):
		status, code = http.StatusConflict, ErrAIDraftAlreadyRunning.Error()
	case errors.Is(err, ErrAIDraftNotRetryable):
		status, code = http.StatusConflict, ErrAIDraftNotRetryable.Error()
	case errors.Is(err, ErrAIDraftNotFound):
		status, code = http.StatusNotFound, ErrAIDraftNotFound.Error()
	default:
		logger.FromContext(r.Context()).Error("[ERROR] " + action + ": " + err.Error())
	}
	models.SendJSON(w, status, "menu", action, map[string]string{"error": code})
}
