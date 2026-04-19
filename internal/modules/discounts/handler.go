package discounts

import (
	"encoding/json"
	"net/http"
	"strings"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{service: svc}
}

// GET /menu/discounts
func (h *Handler) ListActiveDiscounts(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "discounts", "list_active", map[string]string{"error": "missing_token"})
		return
	}

	discounts, err := h.service.GetActiveDiscounts(r.Context(), token)
	if err != nil {
		models.SendErrorJSON(w, "discounts", "list_active", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "discounts", "list_active", discounts)
}

// GET /menu/discounts/all (for back-office)
func (h *Handler) ListAllDiscounts(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "discounts", "list_all", map[string]string{"error": "missing_token"})
		return
	}

	discounts, err := h.service.GetAllDiscounts(r.Context(), token)
	if err != nil {
		models.SendErrorJSON(w, "discounts", "list_all", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "discounts", "list_all", discounts)
}

// GET /menu/discounts/{discount_id}
func (h *Handler) GetDiscount(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "discounts", "get", map[string]string{"error": "missing_token"})
		return
	}

	discountIDStr := chi.URLParam(r, "discount_id")
	if strings.TrimSpace(discountIDStr) == "" {
		models.SendJSON(w, http.StatusBadRequest, "discounts", "get", map[string]string{"error": "missing_discount_id"})
		return
	}

	discount, err := h.service.GetDiscountByID(r.Context(), token, discountIDStr)
	if err != nil {
		models.SendErrorJSON(w, "discounts", "get", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "discounts", "get", discount)
}

// POST /menu/discounts
func (h *Handler) CreateDiscount(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "discounts", "create", map[string]string{"error": "missing_token"})
		return
	}

	var req CreateDiscountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "discounts", "create", map[string]string{"error": "invalid_body", "message": err.Error()})
		return
	}

	discount, err := h.service.CreateDiscount(r.Context(), token, &req)
	if err != nil {
		models.SendErrorJSON(w, "discounts", "create", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "discounts", "create", discount)
}

// PATCH /menu/discounts/{discount_id}
func (h *Handler) UpdateDiscount(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "discounts", "update", map[string]string{"error": "missing_token"})
		return
	}

	discountIDStr := chi.URLParam(r, "discount_id")
	if strings.TrimSpace(discountIDStr) == "" {
		models.SendJSON(w, http.StatusBadRequest, "discounts", "update", map[string]string{"error": "missing_discount_id"})
		return
	}

	var req UpdateDiscountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "discounts", "update", map[string]string{"error": "invalid_body"})
		return
	}

	discount, err := h.service.UpdateDiscount(r.Context(), token, discountIDStr, &req)
	if err != nil {
		models.SendErrorJSON(w, "discounts", "update", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "discounts", "update", discount)
}

// DELETE /menu/discounts/{discount_id}
func (h *Handler) DeleteDiscount(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "discounts", "delete", map[string]string{"error": "missing_token"})
		return
	}

	discountIDStr := chi.URLParam(r, "discount_id")
	if strings.TrimSpace(discountIDStr) == "" {
		models.SendJSON(w, http.StatusBadRequest, "discounts", "delete", map[string]string{"error": "missing_discount_id"})
		return
	}

	if err := h.service.DeleteDiscount(r.Context(), token, discountIDStr); err != nil {
		models.SendErrorJSON(w, "discounts", "delete", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
