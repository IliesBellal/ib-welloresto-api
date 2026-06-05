package performance

import (
	"net/http"
	"strings"

	"welloresto-api/internal/models"
	sharedpkg "welloresto-api/internal/modules/planning/shared"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) GetPlanningPerformance(w http.ResponseWriter, r *http.Request) {
	fromRaw := strings.TrimSpace(r.URL.Query().Get("from"))
	toRaw := strings.TrimSpace(r.URL.Query().Get("to"))
	if fromRaw == "" || toRaw == "" {
		models.SendErrorJSON(w, "planning", "get_planning_performance", models.ErrPlanningInvalidDate)
		return
	}

	fromDate, err := sharedpkg.ParsePlanningDate(fromRaw)
	if err != nil {
		models.SendErrorJSON(w, "planning", "get_planning_performance", models.ErrPlanningInvalidDate)
		return
	}
	toDate, err := sharedpkg.ParsePlanningDate(toRaw)
	if err != nil {
		models.SendErrorJSON(w, "planning", "get_planning_performance", models.ErrPlanningInvalidDate)
		return
	}
	if toDate.Before(fromDate) {
		models.SendErrorJSON(w, "planning", "get_planning_performance", models.ErrPlanningInvalidDate)
		return
	}

	payload, err := h.svc.GetPerformanceByDay(r.Context(), fromDate, toDate)
	if err != nil {
		models.SendErrorJSON(w, "planning", "get_planning_performance", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "planning", "get_planning_performance", map[string]interface{}{
		"status":      "success",
		"performance": payload,
	})
}
