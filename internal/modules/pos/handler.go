package pos

import (
	"encoding/json"
	"net/http"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type POSHandler struct {
	service *POSService
}

func NewPOSHandler(s *POSService) *POSHandler {
	return &POSHandler{service: s}
}

func (h *POSHandler) GetPOSStatus(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "get_pos_status", map[string]string{"error": "missing_token"})
		return
	}

	resp, err := h.service.GetPOSStatus(r.Context(), token)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "get_status", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "get_status", map[string]interface{}{"pos_status": resp})
}

func (h *POSHandler) UpdatePOSStatus(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "update_pos_status", map[string]string{"error": "missing_token"})
		return
	}

	var body struct {
		Status bool `json:"status"`
	}

	json.NewDecoder(r.Body).Decode(&body)

	resp, err := h.service.UpdatePOSStatus(r.Context(), token, body.Status)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "update_status", map[string]string{"error": err.Error()})
		return
	}

	// Return same as GET
	models.SendJSON(w, http.StatusOK, "pos", "update_status", map[string]interface{}{"pos_status": resp})
}

func (h *POSHandler) GetDeletionReasons(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "get_deletion_reasons", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	obj := chi.URLParam(r, "object")

	reasons, err := h.service.GetDeletionReasons(ctx, obj)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "get_deletion_reasons", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "get_deletion_reasons", models.DeletionReasonResponse{
		Status:          "1",
		DeletionReasons: reasons,
	})
}

func (h *POSHandler) ToggleScanNOrder(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "toggle_scannorder", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	var req models.StatusRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "pos", "toggle_scannorder", map[string]string{"error": "invalid_request"})
		return
	}

	updated, err := h.service.ToggleScanNOrder(ctx, token, req.Status)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "toggle_scannorder", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "toggle_scannorder", models.AvailabilityResponse{
		Status:  "1",
		Updated: updated,
	})
}

func (h *POSHandler) ToggleProductionPaidOnly(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "toggle_production_paid_only", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	var req models.StatusRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "pos", "toggle_production_paid_only", map[string]string{"error": "invalid_request"})
		return
	}

	updated, err := h.service.ToggleProductionPaidOnly(ctx, token, req.Status)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "toggle_production_paid_only", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "toggle_production_paid_only", models.AvailabilityResponse{
		Status:  "1",
		Updated: updated,
	})
}

func (h *POSHandler) GetTVARates(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "get_tva_rates", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	updated, err := h.service.GetTVARates(ctx, token)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "get_tva_rates", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "get_tva_rates", updated)
}

func (h *POSHandler) ToggleSafetyStockActive(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "toggle_safety_stock", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	var req models.StatusRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "pos", "toggle_safety_stock", map[string]string{"error": "invalid_request"})
		return
	}

	updated, err := h.service.ToggleSafetyStock(ctx, token, req.Status)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "toggle_safety_stock", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "toggle_safety_stock", models.AvailabilityResponse{
		Status:  "1",
		Updated: updated,
	})
}

func (h *POSHandler) GetDeliveryMen(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "get_users", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	users, err := h.service.GetDeliveryMen(ctx, token)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "get_users", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "get_users", models.DeliveryMenResponse{
		Users: users,
	})
}

func (h *POSHandler) CheckTR(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "check_tr", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()
	code := chi.URLParam(r, "code")

	resp, err := h.service.CheckTR(ctx, token, code)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "check_tr", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "check_tr", resp)
}

func (h *POSHandler) UpdateMerchantSettings(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "update_merchant_settings", map[string]string{"error": "missing_token"})
		return
	}

	var req models.UpdateMerchantSettingsRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "pos", "update_merchant_settings", map[string]string{"error": "invalid_request"})
		return
	}

	if err := h.service.UpdateMerchantSettings(r.Context(), token, &req); err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "update_merchant_settings", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "pos", "update_merchant_settings", map[string]string{"status": "ok"})
}

func (h *POSHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		models.SendJSON(w, http.StatusUnauthorized, "pos", "get_settings", map[string]string{"error": "missing_token"})
		return
	}

	ctx := r.Context()

	resp, err := h.service.GetMerchantSettingsV2(ctx, token)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "pos", "get_settings", map[string]string{"status": "-2", "error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
