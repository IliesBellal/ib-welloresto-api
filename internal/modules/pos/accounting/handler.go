package accounting

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

// AccountingHandler expose l'export comptable. r2Client est le client du
// bucket R2 PRIVÉ (R2_PRIVATE_BUCKET) : les exports n'y sont lisibles que par
// lien signé (migration 166).
type AccountingHandler struct {
	service  *AccountingService
	r2Client *r2.Client
}

func NewAccountingHandler(svc *AccountingService, privateR2Client *r2.Client) *AccountingHandler {
	return &AccountingHandler{service: svc, r2Client: privateR2Client}
}

// ExportOptions GET /pos/accounting/export-options?date_from=YYYY-MM-DD — mode
// de clôture de l'établissement au premier jour de la période et canaux
// filtrables (vides en clôture manuelle).
func (h *AccountingHandler) ExportOptions(w http.ResponseWriter, r *http.Request) {
	resp, err := h.service.ExportOptions(r.Context(), r.URL.Query().Get("date_from"))
	if err != nil {
		models.SendErrorJSON(w, "pos", "accounting_export_options", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "pos", "accounting_export_options", resp)
}

// ListAccountingExports GET /pos/accounting/exports — exports archivés de
// l'établissement, du plus récent au plus ancien.
func (h *AccountingHandler) ListAccountingExports(w http.ResponseWriter, r *http.Request) {
	resp, err := h.service.ListAccountingExports(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "pos", "accounting_exports_list", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "pos", "accounting_exports_list", resp)
}

// AccountingExportDownload GET /pos/accounting/exports/{export_id}/download —
// nouveau lien signé (une heure) vers un export archivé de l'établissement.
func (h *AccountingHandler) AccountingExportDownload(w http.ResponseWriter, r *http.Request) {
	exportID, err := strconv.ParseInt(chi.URLParam(r, "export_id"), 10, 64)
	if err != nil {
		models.SendErrorJSON(w, "pos", "accounting_export_download", models.ErrMissingResourceID)
		return
	}
	resp, err := h.service.AccountingExportLink(r.Context(), exportID, h.r2Client)
	if err != nil {
		models.SendErrorJSON(w, "pos", "accounting_export_download", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "pos", "accounting_export_download", resp)
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
	report, err := h.service.ExportAccountingReport(ctx, token, req.DateFrom, req.DateTo, req.Channels, h.r2Client)
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

// ExportRegisterPDF POST /accounting/registers/{register_id}/export-pdf
func (h *AccountingHandler) ExportRegisterPDF(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "accounting", "register_export_pdf", map[string]string{"error": "missing_token"})
		return
	}

	registerID := chi.URLParam(r, "register_id")
	if strings.TrimSpace(registerID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "accounting", "register_export_pdf", map[string]string{"error": "invalid_request"})
		return
	}

	pdfBytes, filename, err := h.service.ExportRegisterPDF(r.Context(), registerID)
	if err != nil {
		models.SendErrorJSON(w, "accounting", "register_export_pdf", err)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdfBytes)
}
