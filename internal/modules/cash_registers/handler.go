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
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}

	ctx := r.Context()

	var req models.OpenCashRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	open_call, err := h.cashRegisterService.OpenCashRegister(ctx, token, &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		models.SendJSON(w, "cash_register", "open_error", map[string]string{"error": "internal error: " + err.Error()})
		return
	}

	models.SendJSON(w, "cash_register", "open", open_call)
}

func (h *CashRegisterHandler) CloseCashRegister(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}

	ctx := r.Context()

	cashRegisterID := chi.URLParam(r, "cash_register_id")
	if cashRegisterID == "" {
		http.Error(w, "missing cash_register_id", http.StatusBadRequest)
		return
	}

	var req models.CloseCashRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	resp, err := h.cashRegisterService.CloseCashRegister(ctx, token, cashRegisterID, &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		models.SendJSON(w, "cash_register", "close_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "cash_register", "close", resp)
}

func (h *CashRegisterHandler) GetCashRegisterSummary(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}

	ctx := r.Context()

	cashRegisterID := chi.URLParam(r, "cash_register_id")
	if cashRegisterID == "" {
		http.Error(w, "missing cash_register_id", http.StatusBadRequest)
		return
	}

	summary, err := h.cashRegisterService.GetCashRegisterSummary(ctx, token, cashRegisterID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		models.SendJSON(w, "cash_register", "get_summary_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "cash_register", "get_summary", summary)
}

func (h *CashRegisterHandler) GetCashRegisterTVADetails(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}

	ctx := r.Context()

	cashRegisterID := chi.URLParam(r, "cash_register_id")
	if cashRegisterID == "" {
		http.Error(w, "missing cash_register_id", http.StatusBadRequest)
		return
	}

	resp, err := h.cashRegisterService.GetCashRegisterTVADetails(ctx, token, cashRegisterID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		models.SendJSON(w, "cash_register", "get_tva_details_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "cash_register", "get_tva_details", resp)
}

func (h *CashRegisterHandler) AddCustomItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "cash_register_id")

	var req models.AddCustomItemRequest
	json.NewDecoder(r.Body).Decode(&req)

	resp, err := h.cashRegisterService.AddCustomItem(r.Context(), id, &req)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		models.SendJSON(w, "cash_register", "add_custom_item_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "cash_register", "add_custom_item", resp)
}

func (h *CashRegisterHandler) DeleteCustomItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "cash_register_id")
	itemID := chi.URLParam(r, "item_id")

	resp, err := h.cashRegisterService.DeleteCustomItem(r.Context(), id, itemID)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		models.SendJSON(w, "cash_register", "delete_custom_item_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "cash_register", "delete_custom_item", resp)
}

func (h *CashRegisterHandler) EncloseCashRegister(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}

	ctx := r.Context()
	id := chi.URLParam(r, "cash_register_id")

	var req models.EncloseCashRegisterRequest
	json.NewDecoder(r.Body).Decode(&req)

	resp, err := h.cashRegisterService.EncloseCashRegister(ctx, id, token, req.Comment)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		models.SendJSON(w, "cash_register", "enclose_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "cash_register", "enclose", resp)
}

func (h *CashRegisterHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}

	ctx := r.Context()

	result, err := h.cashRegisterService.GetCashRegisterHistory(ctx, token)
	if err != nil {
		models.SendJSON(w, "cash_register", "get_history_error", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, "cash_register", "get_history", models.CashRegisterHistoryResponse{
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
	models.SendJSON(w, "cash_drawer", "open", map[string]string{"status": "1"})
}
