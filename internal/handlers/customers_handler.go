package handlers

import (
	"encoding/json"
	"net/http"
	"welloresto-api/internal/models"
	"welloresto-api/internal/services"

	"github.com/go-chi/chi/v5"
)

type CustomersHandler struct {
	svc *services.CustomersService
}

func NewCustomersHandler(s *services.CustomersService) *CustomersHandler {
	return &CustomersHandler{svc: s}
}

func (h *CustomersHandler) GetCustomerLoyalty(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := extractToken(r)
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
	token := extractToken(r)

	var req models.LoyaltyProgressUpdateRequest
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
	token := extractToken(r)

	var req models.LoyaltyRewardUpdateRequest
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
	token := extractToken(r)

	params := models.CustomerSearchRequest{
		Name:    r.URL.Query().Get("name"),
		Tel:     r.URL.Query().Get("tel"),
		Address: r.URL.Query().Get("address"),
		Code:    r.URL.Query().Get("code"),
	}

	customers, err := h.svc.SearchCustomers(ctx, token, &params)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "1",
		"exist":  len(customers) > 0,
		"result": customers,
	})
}
