package reports

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

type ReportsHandler struct {
	service *ReportsService
}

func NewReportsHandler(svc *ReportsService) *ReportsHandler {
	return &ReportsHandler{service: svc}
}

// GetTVAReport POST /pos/reports/tva
func (h *ReportsHandler) GetTVAReport(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "reports_tva", map[string]string{"error": "missing_token"})
		return
	}

	var req ReportsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "pos", "reports_tva", map[string]string{"error": "invalid_request"})
		return
	}

	ctx := r.Context()
	report, err := h.service.GetTVAReport(ctx, token, req.DateFrom, req.DateTo)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "reports_tva", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "reports_tva", report)
}

// GetPaymentsReport POST /pos/reports/payments
func (h *ReportsHandler) GetPaymentsReport(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "establishment", "report_payments", map[string]string{"error": "missing_token"})
		return
	}

	var req ReportsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "establishment", "report_payments", map[string]string{"error": "invalid_request"})
		return
	}

	ctx := r.Context()
	report, err := h.service.GetPaymentsReport(ctx, token, req.DateFrom, req.DateTo)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "establishment", "report_payments", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "establishment", "report_payments", report)
}
