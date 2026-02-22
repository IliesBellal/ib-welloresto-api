package cash_registers

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

// LocationsHandler handles orders endpoints
type CashRegisterHandler struct {
	cashRegisterService *CashRegisterService
}

func NewCashRegisterHandler(cashRegisterService *CashRegisterService) *CashRegisterHandler {
	return &CashRegisterHandler{
		cashRegisterService: cashRegisterService,
	}
}

func (h *CashRegisterHandler) OpenCashRegister(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "open", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	var req models.OpenCashRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "cash_register", "open", map[string]string{"error": "invalid_body"})
		return
	}

	open_call, err := h.cashRegisterService.OpenCashRegister(ctx, token, &req)
	if err != nil {
		models.SendErrorJSON(w, "cash_register", "open", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "open", open_call)
}

func (h *CashRegisterHandler) CloseCashRegister(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "close", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	cashRegisterID := chi.URLParam(r, "cash_register_id")
	if cashRegisterID == "" {
		models.SendJSON(w, http.StatusBadRequest, "cash_register", "close", map[string]string{"error": "missing_parameter"})
		return
	}

	var req models.CloseCashRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "cash_register", "close", map[string]string{"error": "invalid_body"})
		return
	}

	resp, err := h.cashRegisterService.CloseCashRegister(ctx, token, cashRegisterID, &req)
	if err != nil {
		models.SendErrorJSON(w, "cash_register", "close", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "close", resp)
}

func (h *CashRegisterHandler) GetCashRegisterSummary(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "get_summary", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	cashRegisterID := chi.URLParam(r, "cash_register_id")
	if cashRegisterID == "" {
		models.SendJSON(w, http.StatusBadRequest, "cash_register", "get_summary", map[string]string{"error": "missing_parameter"})
		return
	}

	summary, err := h.cashRegisterService.GetCashRegisterSummary(ctx, token, cashRegisterID)
	if err != nil {
		models.SendErrorJSON(w, "cash_register", "get_summary", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "get_summary", summary)
}

func (h *CashRegisterHandler) GetCashRegisterTVADetails(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "get_tva_details", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	cashRegisterID := chi.URLParam(r, "cash_register_id")
	if cashRegisterID == "" {
		models.SendJSON(w, http.StatusBadRequest, "cash_register", "get_tva_details", map[string]string{"error": "missing_parameter"})
		return
	}

	resp, err := h.cashRegisterService.GetCashRegisterTVADetails(ctx, token, cashRegisterID)
	if err != nil {
		models.SendErrorJSON(w, "cash_register", "get_tva_details", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "get_tva_details", resp)
}

func (h *CashRegisterHandler) AddCustomItem(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "add_custom_item", map[string]string{"error": "missing_token"})
		return
	}

	id := chi.URLParam(r, "cash_register_id")

	var req models.AddCustomItemRequest
	json.NewDecoder(r.Body).Decode(&req)

	resp, err := h.cashRegisterService.AddCustomItem(r.Context(), token, id, &req)

	if err != nil {
		models.SendErrorJSON(w, "cash_register", "add_custom_item", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "add_custom_item", resp)
}

func (h *CashRegisterHandler) DeleteCustomItem(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "delete_custom_item", map[string]string{"error": "missing_token"})
		return
	}

	id := chi.URLParam(r, "cash_register_id")
	itemID := chi.URLParam(r, "item_id")

	resp, err := h.cashRegisterService.DeleteCustomItem(r.Context(), token, id, itemID)
	if err != nil {
		models.SendErrorJSON(w, "cash_register", "delete_custom_item", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "delete_custom_item", resp)
}

func (h *CashRegisterHandler) EncloseCashRegister(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "enclose", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	id := chi.URLParam(r, "cash_register_id")

	var req models.EncloseCashRegisterRequest
	json.NewDecoder(r.Body).Decode(&req)

	resp, err := h.cashRegisterService.EncloseCashRegister(ctx, id, token, req.Comment)

	if err != nil {
		models.SendErrorJSON(w, "cash_register", "enclose", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "enclose", resp)
}

func (h *CashRegisterHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_register", "get_history", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	result, err := h.cashRegisterService.GetCashRegisterHistory(ctx, token)
	if err != nil {
		models.SendErrorJSON(w, "cash_register", "get_history", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_register", "get_history", models.CashRegisterHistoryResponse{
		Status:        "1",
		CashRegisters: result,
	})
}

func (h *CashRegisterHandler) json(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *CashRegisterHandler) errorJSON(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "0",
		"error":  err.Error(),
	})
}

func (h *CashRegisterHandler) OpenCashDrawer(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "cash_drawer", "open", map[string]string{"error": "missing_token"})
		return
	}

	models.SendJSON(w, http.StatusOK, "cash_drawer", "open", map[string]string{"status": "1"})
}
