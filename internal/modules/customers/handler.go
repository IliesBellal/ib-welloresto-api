package customers

import (
	"encoding/json"
	"net/http"
	"strings"
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
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "customers", "get_loyalty", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	customerID := chi.URLParam(r, "customer_id")

	result, err := h.svc.GetCustomerLoyalty(ctx, token, customerID)
	if err != nil {
		models.SendErrorJSON(w, "customers", "get_loyalty", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "customers", "get_loyalty", map[string]interface{}{
		"status":  "1",
		"loyalty": result,
	})
}

func (h *CustomersHandler) UpdateLoyaltyProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "customers", "update_loyalty_progress", map[string]string{"error": "missing_token"})
		return
	}

	var req LoyaltyProgressUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "customers", "update_loyalty_progress", map[string]string{"error": "invalid_request"})
		return
	}

	result, err := h.svc.UpdateLoyaltyProgress(ctx, token, &req)
	if err != nil {
		models.SendErrorJSON(w, "customers", "update_loyalty_progress", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "customers", "update_loyalty_progress", result)
}

func (h *CustomersHandler) UpdateLoyaltyReward(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "customers", "update_loyalty_reward", map[string]string{"error": "missing_token"})
		return
	}

	var req LoyaltyRewardUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "customers", "update_loyalty_reward", map[string]string{"error": "invalid_request"})
		return
	}

	result, err := h.svc.UpdateLoyaltyReward(ctx, token, &req)
	if err != nil {
		models.SendErrorJSON(w, "customers", "update_loyalty_reward", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "customers", "update_loyalty_reward", result)
}

func (h *CustomersHandler) SearchCustomers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "customers", "search", map[string]string{"error": "missing_token"})
		return
	}

	search_term := r.URL.Query().Get("term")

	customers, err := h.svc.SearchCustomers(ctx, token, search_term)
	if err != nil {
		models.SendErrorJSON(w, "customers", "search", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "customers", "search", map[string]interface{}{
		"status": "success",
		"result": customers,
	})
}

func (h *CustomersHandler) ListCustomers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "customers", "list", map[string]string{"error": "missing_token"})
		return
	}

	page := r.URL.Query().Get("page")
	if page == "" {
		page = "0"
	}
	page_size := r.URL.Query().Get("page_size")
	if page_size == "" {
		page_size = "10"
	}

	customers, err := h.svc.ListCustomers(ctx, token, helpers.StringToInt(page), helpers.StringToInt(page_size))
	if err != nil {
		models.SendErrorJSON(w, "customers", "list", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "customers", "list", map[string]interface{}{
		"status": "success",
		"result": customers,
	})
}
