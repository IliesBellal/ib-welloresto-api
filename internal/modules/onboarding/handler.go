package onboarding

import (
	"net/http"

	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// GetOnboarding handles GET /v1/merchants/{id}/onboarding.
func (h *Handler) GetOnboarding(w http.ResponseWriter, r *http.Request) {
	merchantID := chi.URLParam(r, "id")
	tasks, err := h.svc.GetOnboarding(r.Context(), merchantID)
	if err != nil {
		models.SendErrorJSON(w, "onboarding", "get", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "onboarding", "get", map[string]interface{}{"status": "success", "tasks": tasks})
}
