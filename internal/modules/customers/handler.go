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
		"status":  "success",
		"loyalty": result,
	})
}

func (h *CustomersHandler) GetLoyaltyPrograms(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "customers", "get_loyalty_programs", map[string]string{"error": "missing_token"})
		return
	}

	result, err := h.svc.GetLoyaltyPrograms(ctx, token)
	if err != nil {
		models.SendErrorJSON(w, "customers", "get_loyalty_programs", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "customers", "get_loyalty_programs", result)
}

func (h *CustomersHandler) UpdateLoyaltyProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "customers", "update_loyalty_progress", map[string]string{"error": "missing_token"})
		return
	}

	customerID := strings.TrimSpace(chi.URLParam(r, "customer_id"))
	loyaltyProgramID := strings.TrimSpace(chi.URLParam(r, "loyalty_program_id"))
	if customerID == "" || loyaltyProgramID == "" {
		models.SendJSON(w, http.StatusBadRequest, "customers", "update_loyalty_progress", map[string]string{"error": "missing_path_params"})
		return
	}

	var body struct {
		CurrentValue int `json:"current_value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "customers", "update_loyalty_progress", map[string]string{"error": "invalid_request"})
		return
	}

	req := LoyaltyProgressUpdateRequest{
		CustomerID:       customerID,
		LoyaltyProgramID: loyaltyProgramID,
		CurrentValue:     body.CurrentValue,
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

	customerID := strings.TrimSpace(chi.URLParam(r, "customer_id"))
	rewardID := strings.TrimSpace(chi.URLParam(r, "reward_id"))
	if customerID == "" || rewardID == "" {
		models.SendJSON(w, http.StatusBadRequest, "customers", "update_loyalty_reward", map[string]string{"error": "missing_path_params"})
		return
	}

	var body struct {
		IsUsed bool `json:"is_used"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "customers", "update_loyalty_reward", map[string]string{"error": "invalid_request"})
		return
	}

	req := LoyaltyRewardUpdateRequest{
		CustomerID: customerID,
		RewardID:   rewardID,
		IsUsed:     body.IsUsed,
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
	sortField := r.URL.Query().Get("sort_field")
	sortDir := r.URL.Query().Get("sort_dir")

	page := helpers.StringToInt(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}

	pageSize := helpers.StringToInt(r.URL.Query().Get("page_size"))
	if pageSize <= 0 {
		pageSize = 10
	}

	customerData, err := h.svc.SearchCustomers(ctx, token, search_term, sortField, sortDir, page, pageSize)
	if err != nil {
		models.SendErrorJSON(w, "customers", "search", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "customers", "search", customerData)
}

func (h *CustomersHandler) ListCustomers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "customers", "list", map[string]string{"error": "missing_token"})
		return
	}

	sortField := r.URL.Query().Get("sort_field")
	sortDir := r.URL.Query().Get("sort_dir")

	page := helpers.StringToInt(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}

	pageSize := helpers.StringToInt(r.URL.Query().Get("page_size"))
	if pageSize <= 0 {
		pageSize = 10
	}

	customerData, err := h.svc.ListCustomers(ctx, token, sortField, sortDir, page, pageSize)
	if err != nil {
		models.SendErrorJSON(w, "customers", "list", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "customers", "list", customerData)
}
