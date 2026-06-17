package printers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

type Handler struct {
	service *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{service: svc}
}

// GET /printers
func (h *Handler) ListPrinters(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "printers", "list", map[string]string{"error": "missing_token"})
		return
	}

	printerList, err := h.service.ListPrinters(r.Context(), token)
	if err != nil {
		models.SendErrorJSON(w, "printers", "list", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "printers", "list", printerList)
}

// POST /printers
func (h *Handler) CreatePrinter(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "printers", "create", map[string]string{"error": "missing_token"})
		return
	}

	var req CreatePrinterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "printers", "create", map[string]string{"error": "invalid_request"})
		return
	}

	printer, err := h.service.CreatePrinter(r.Context(), token, &req)
	if err != nil {
		models.SendErrorJSON(w, "printers", "create", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "printers", "create", printer)
}

// PATCH /printers/{printer_id}
func (h *Handler) UpdatePrinter(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "printers", "update", map[string]string{"error": "missing_token"})
		return
	}

	printerID := chi.URLParam(r, "printer_id")
	if strings.TrimSpace(printerID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "printers", "update", map[string]string{"error": "missing_printer_id"})
		return
	}

	var req UpdatePrinterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "printers", "update", map[string]string{"error": "invalid_request"})
		return
	}

	printer, err := h.service.UpdatePrinter(r.Context(), token, printerID, &req)
	if err != nil {
		models.SendErrorJSON(w, "printers", "update", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "printers", "update", printer)
}

// DELETE /printers/{printer_id}
func (h *Handler) DeletePrinter(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "printers", "delete", map[string]string{"error": "missing_token"})
		return
	}

	printerID := chi.URLParam(r, "printer_id")
	if strings.TrimSpace(printerID) == "" {
		models.SendJSON(w, http.StatusBadRequest, "printers", "delete", map[string]string{"error": "missing_printer_id"})
		return
	}

	if err := h.service.DeletePrinter(r.Context(), token, printerID); err != nil {
		models.SendErrorJSON(w, "printers", "delete", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
