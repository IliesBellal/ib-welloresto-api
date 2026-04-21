package accounting

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/models"
)

type AccountingHandler struct {
	service  *AccountingService
	r2Client *r2.Client
}

func NewAccountingHandler(svc *AccountingService, r2Client *r2.Client) *AccountingHandler {
	return &AccountingHandler{service: svc, r2Client: r2Client}
}

// ExportAccounting POST /pos/accounting/export
func (h *AccountingHandler) ExportAccounting(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "accounting_export", map[string]string{"error": "missing_token"})
		return
	}

	var req ExportAccountingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "pos", "accounting_export", map[string]string{"error": "invalid_request"})
		return
	}

	ctx := r.Context()
	report, err := h.service.ExportAccountingReport(ctx, token, req.DateFrom, req.DateTo, h.r2Client)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "accounting_export", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "accounting_export", report)
}
