package accounting

import (
	"encoding/json"
	"fmt"
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

func (h *AccountingHandler) CalculateVAT(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "accounting", "vat_calculate", map[string]string{"error": "missing_token"})
		return
	}

	var req VATCalculateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "accounting", "vat_calculate", err)
		return
	}

	resp, err := h.service.CalculateVAT(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "accounting", "vat_calculate", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "accounting", "vat_calculate", resp)
}

func (h *AccountingHandler) ExportVATCSV(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "accounting", "vat_export_csv", map[string]string{"error": "missing_token"})
		return
	}

	var req VATCalculateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "accounting", "vat_export_csv", err)
		return
	}

	csvData, filename, err := h.service.ExportVATCSV(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "accounting", "vat_export_csv", err)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(csvData)
}
