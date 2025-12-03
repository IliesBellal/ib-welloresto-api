package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/models"
	"welloresto-api/internal/services"

	"github.com/go-chi/chi/v5"
)

// LocationsHandler handles orders endpoints
type CashRegisterHandler struct {
	cashRegisterService *services.CashRegisterService
}

func NewCashRegisterHandler(cashRegisterService *services.CashRegisterService) *CashRegisterHandler {
	return &CashRegisterHandler{
		cashRegisterService: cashRegisterService,
	}
}

func (h *CashRegisterHandler) OpenCashRegister(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
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

	resp, err := h.cashRegisterService.OpenCashRegister(ctx, token, &req)
	if err != nil {
		http.Error(w, "internal error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (h *CashRegisterHandler) CloseCashRegister(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (h *CashRegisterHandler) GetCashRegisterSummary(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
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

	resp, err := h.cashRegisterService.GetCashRegisterSummary(ctx, token, cashRegisterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (h *CashRegisterHandler) AddCustomItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "cash_register_id")

	var req models.AddCustomItemRequest
	json.NewDecoder(r.Body).Decode(&req)

	resp, err := h.cashRegisterService.AddCustomItem(r.Context(), id, &req)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (h *CashRegisterHandler) DeleteCustomItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "cash_register_id")
	itemID := chi.URLParam(r, "item_id")

	resp, err := h.cashRegisterService.DeleteCustomItem(r.Context(), id, itemID)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (h *CashRegisterHandler) EncloseCashRegister(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (h *CashRegisterHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, 401)
		return
	}

	ctx := r.Context()

	result, err := h.cashRegisterService.GetCashRegisterHistory(ctx, token)
	if err != nil {
		h.errorJSON(w, err)
		return
	}

	h.json(w, models.CashRegisterHistoryResponse{
		Status:        "1",
		CashRegisters: result,
	}, 200)
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
