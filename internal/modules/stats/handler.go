package stats

import (
	"net/http"
	"strings"
	"time"
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

// GetUpsellStats GET /stats/upsell?from=YYYY-MM-DD&to=YYYY-MM-DD
func (h *StatsHandler) GetUpsellStats(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "stats", "get_upsell_stats", map[string]string{"error": "missing_token"})
		return
	}

	fromRaw := strings.TrimSpace(r.URL.Query().Get("from"))
	toRaw := strings.TrimSpace(r.URL.Query().Get("to"))
	if fromRaw == "" || toRaw == "" {
		models.SendJSON(w, http.StatusBadRequest, "stats", "get_upsell_stats", map[string]string{"error": "missing_period"})
		return
	}

	fromDate, err := time.Parse("2006-01-02", fromRaw)
	if err != nil {
		models.SendJSON(w, http.StatusBadRequest, "stats", "get_upsell_stats", map[string]string{"error": "invalid_from"})
		return
	}
	toDate, err := time.Parse("2006-01-02", toRaw)
	if err != nil {
		models.SendJSON(w, http.StatusBadRequest, "stats", "get_upsell_stats", map[string]string{"error": "invalid_to"})
		return
	}
	if toDate.Before(fromDate) {
		models.SendJSON(w, http.StatusBadRequest, "stats", "get_upsell_stats", map[string]string{"error": "invalid_period"})
		return
	}

	ctx := r.Context()
	stats, err := h.service.GetUpsellStats(ctx, token, fromDate, toDate)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "stats", "get_upsell_stats", map[string]interface{}{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "stats", "get_upsell_stats", stats)
}
