package onboarding

import (
	"encoding/json"
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

type skipTaskRequest struct {
	Reason string `json:"reason"`
}

// SkipTask handles POST /v1/merchants/{id}/onboarding/{code}/skip.
func (h *Handler) SkipTask(w http.ResponseWriter, r *http.Request) {
	merchantID := chi.URLParam(r, "id")
	taskKey := chi.URLParam(r, "code")

	var req skipTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "onboarding", "skip", models.ErrInvalidInput)
		return
	}

	if err := h.svc.SkipTask(r.Context(), merchantID, taskKey, req.Reason); err != nil {
		models.SendErrorJSON(w, "onboarding", "skip", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "onboarding", "skip", map[string]interface{}{"status": "success"})
}
