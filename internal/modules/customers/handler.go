package customers

import (
	"encoding/json"
	"net/http"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type CustomersHandler struct {
	svc *CustomersService
}

func NewCustomersHandler(s *CustomersService) *CustomersHandler {
	return &CustomersHandler{svc: s}
}

func (h *CustomersHandler) GetCustomerLoyalty(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := helpers.ExtractToken(r)
	customerID := chi.URLParam(r, "customer_id")

	result, err := h.svc.GetCustomerLoyalty(ctx, token, customerID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "1",
		"loyalty": result,
	})
}

func (h *CustomersHandler) UpdateLoyaltyProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := helpers.ExtractToken(r)

	var req LoyaltyProgressUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}

	result, err := h.svc.UpdateLoyaltyProgress(ctx, token, &req)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	json.NewEncoder(w).Encode(result)
}

func (h *CustomersHandler) UpdateLoyaltyReward(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := helpers.ExtractToken(r)

	var req LoyaltyRewardUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", 400)
		return
	}

	result, err := h.svc.UpdateLoyaltyReward(ctx, token, &req)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	json.NewEncoder(w).Encode(result)
}

func (h *CustomersHandler) SearchCustomers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := helpers.ExtractToken(r)

	search_term := r.URL.Query().Get("term")

	customers, err := h.svc.SearchCustomers(ctx, token, search_term)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	resp := models.HandlerDefaultResponse{
		ID: "customer.search",
		Data: map[string]interface{}{
			"status": "success",
			"result": customers,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
