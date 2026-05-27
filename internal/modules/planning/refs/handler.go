package refs

import (
	"net/http"

	"welloresto-api/internal/models"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListContractTypes(w http.ResponseWriter, r *http.Request) {
	refs, err := h.svc.ListContractTypes(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_contract_types", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_contract_types", map[string]interface{}{"status": "success", "contract_types": refs})
}

func (h *Handler) ListTimeTrackingModes(w http.ResponseWriter, r *http.Request) {
	refs, err := h.svc.ListTimeTrackingModes(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_time_tracking_modes", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_time_tracking_modes", map[string]interface{}{"status": "success", "time_tracking_modes": refs})
}

func (h *Handler) ListPlanningEventTypes(w http.ResponseWriter, r *http.Request) {
	refs, err := h.svc.ListPlanningEventTypes(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "planning", "list_planning_event_types", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "list_planning_event_types", map[string]interface{}{"status": "success", "planning_event_types": refs})
}
