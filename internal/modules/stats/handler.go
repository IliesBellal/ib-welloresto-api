package stats

import (
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

type StatsHandler struct {
	service *StatsService
}

func NewStatsHandler(svc *StatsService) *StatsHandler {
	return &StatsHandler{service: svc}
}

// GetDashboardSummary GET /stats/dashboard/summary
func (h *StatsHandler) GetDashboardSummary(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "stats", "get_dashboard_summary", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	summary, err := h.service.GetDashboardSummary(ctx, token)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "stats", "get_dashboard_summary", map[string]interface{}{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "stats", "get_dashboard_summary", summary)
}
