package menu

import (
	"encoding/json"
	"errors"
	"net/http"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
)

// CompositionAIHandler expose la composition par IA d'un produit existant.
type CompositionAIHandler struct {
	service *CompositionAIService
}

func NewCompositionAIHandler(s *CompositionAIService) *CompositionAIHandler {
	return &CompositionAIHandler{service: s}
}

// PreviewCompositions — POST /menu/products/composition/ai/preview,
// corps {"product_ids": [...]}. Consomme un crédit d'import photo.
func (h *CompositionAIHandler) PreviewCompositions(w http.ResponseWriter, r *http.Request) {
	const action = "preview_ai_compositions"
	var req AICompositionPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		aiBadRequest(w, action, ErrCompositionInvalidRequest.Error())
		return
	}
	resp, err := h.service.PreviewCompositions(r.Context(), req)
	if err != nil {
		h.sendError(w, r, action, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "menu", action, resp)
}

// ApplyCompositions — POST /menu/products/composition/ai/apply. Aucun appel IA.
func (h *CompositionAIHandler) ApplyCompositions(w http.ResponseWriter, r *http.Request) {
	const action = "apply_ai_compositions"
	var req AICompositionApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		aiBadRequest(w, action, ErrCompositionInvalidRequest.Error())
		return
	}
	resp, err := h.service.ApplyCompositions(r.Context(), req)
	if err != nil {
		h.sendError(w, r, action, err)
		return
	}
	models.SendJSON(w, http.StatusOK, "menu", action, resp)
}

func (h *CompositionAIHandler) sendError(w http.ResponseWriter, r *http.Request, action string, err error) {
	status, code := http.StatusInternalServerError, "internal_error"
	switch {
	case errors.Is(err, ErrCompositionInvalidRequest), errors.Is(err, ErrCompositionTooManyProducts),
		errors.Is(err, ErrCompositionNothingToRead):
		status, code = http.StatusBadRequest, err.Error()
	case errors.Is(err, models.ErrForbidden):
		status, code = http.StatusNotFound, "product_not_found"
	case errors.Is(err, ErrCompositionAIDisabled):
		status, code = http.StatusServiceUnavailable, ErrCompositionAIDisabled.Error()
	case errors.Is(err, ErrAICreditsExhausted):
		status, code = http.StatusPaymentRequired, ErrAICreditsExhausted.Error()
	case errors.Is(err, ErrAIDraftAlreadyRunning):
		status, code = http.StatusConflict, ErrAIDraftAlreadyRunning.Error()
	case errors.Is(err, ErrCompositionAIFailed):
		status, code = http.StatusBadGateway, ErrCompositionAIFailed.Error()
	default:
		logger.FromContext(r.Context()).Error("[ERROR] " + action + ": " + err.Error())
	}
	models.SendJSON(w, status, "menu", action, map[string]string{"error": code})
}
