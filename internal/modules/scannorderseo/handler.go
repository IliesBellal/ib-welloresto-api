package scannorderseo

import (
	"encoding/json"
	"errors"
	"net/http"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

const (
	module = "scannorder_seo"
	// maxBodyBytes : titre, description, trois codes et huit liens tiennent
	// largement dans 16 Ko.
	maxBodyBytes = 16 << 10
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// MerchantRateKey : clé de limitation par établissement, pour la suggestion
// IA (appel payant). La route est derrière l'authentification.
func MerchantRateKey(r *http.Request) (string, error) {
	if user := middleware.GetUser(r); user != nil {
		return "seo-suggest:" + user.MerchantID, nil
	}
	return "seo-suggest:anonymous", nil
}

// GetSettings handles GET /integrations/scannorder/seo
func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	settings, err := h.svc.GetSettings(r.Context(), user.MerchantID)
	if err != nil {
		models.SendErrorJSON(w, module, "get_settings", err)
		return
	}
	models.SendJSON(w, http.StatusOK, module, "get_settings", settings)
}

// UpdateSettings handles PATCH /integrations/scannorder/seo
func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	var req UpdateRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, module, "update_settings", map[string]string{"error": "invalid_body"})
		return
	}

	settings, err := h.svc.UpdateSettings(r.Context(), user.MerchantID, &req)
	var fieldErr *FieldError
	switch {
	case errors.As(err, &fieldErr):
		models.SendJSON(w, http.StatusBadRequest, module, "update_settings", map[string]any{
			"error": "invalid_field", "field": fieldErr.Field, "reason": fieldErr.Reason, "value": fieldErr.Value,
		})
	case err != nil:
		models.SendErrorJSON(w, module, "update_settings", err)
	default:
		models.SendJSON(w, http.StatusOK, module, "update_settings", settings)
	}
}

// Suggest handles POST /integrations/scannorder/seo/suggest
func (h *Handler) Suggest(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	suggestion, err := h.svc.Suggest(r.Context(), user.MerchantID)
	switch {
	case errors.Is(err, ErrSuggestDisabled):
		models.SendJSON(w, http.StatusServiceUnavailable, module, "suggest", map[string]string{"error": ErrSuggestDisabled.Error()})
	case errors.Is(err, ErrMenuTooSmall):
		models.SendJSON(w, http.StatusUnprocessableEntity, module, "suggest", map[string]string{"error": ErrMenuTooSmall.Error()})
	case errors.Is(err, ErrSuggestFailed):
		models.SendJSON(w, http.StatusBadGateway, module, "suggest", map[string]string{"error": ErrSuggestFailed.Error()})
	case err != nil:
		models.SendErrorJSON(w, module, "suggest", err)
	default:
		models.SendJSON(w, http.StatusOK, module, "suggest", suggestion)
	}
}
