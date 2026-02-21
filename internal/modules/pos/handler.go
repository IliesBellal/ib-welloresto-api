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
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	resp, err := h.service.GetPOSStatus(r.Context(), token)
	if err != nil {
		models.SendJSON(w, "pos", "get_status_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "pos", "get_status", map[string]interface{}{"pos_status": resp})
}

func (h *POSHandler) UpdatePOSStatus(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	var body struct {
		Status bool `json:"status"`
	}

	json.NewDecoder(r.Body).Decode(&body)

	resp, err := h.service.UpdatePOSStatus(r.Context(), token, body.Status)
	if err != nil {
		models.SendJSON(w, "pos", "update_status_error", map[string]string{"error": err.Error()})
		return
	}

	// Return same as GET
	models.SendJSON(w, "pos", "update_status", map[string]interface{}{"pos_status": resp})
}

func (h *POSHandler) GetDeletionReasons(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	obj := chi.URLParam(r, "object")

	reasons, err := h.service.GetDeletionReasons(ctx, obj)
	if err != nil {
		models.SendJSON(w, "pos", "get_deletion_reasons_error", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, "pos", "get_deletion_reasons", models.DeletionReasonResponse{
		Status:          "1",
		DeletionReasons: reasons,
	})
}

func (h *POSHandler) ToggleScanNOrder(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	var req models.StatusRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorJSON(w, err)
		return
	}

	updated, err := h.service.ToggleScanNOrder(ctx, token, req.Status)
	if err != nil {
		models.SendJSON(w, "pos", "toggle_scannorder_error", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, "pos", "toggle_scannorder", models.AvailabilityResponse{
		Status:  "1",
		Updated: updated,
	})
}

func (h *POSHandler) ToggleProductionPaidOnly(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	var req models.StatusRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorJSON(w, err)
		return
	}

	updated, err := h.service.ToggleProductionPaidOnly(ctx, token, req.Status)
	if err != nil {
		models.SendJSON(w, "pos", "toggle_production_paid_only_error", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, "pos", "toggle_production_paid_only", models.AvailabilityResponse{
		Status:  "1",
		Updated: updated,
	})
}

func (h *POSHandler) GetTVARates(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	updated, err := h.service.GetTVARates(ctx, token)
	if err != nil {
		models.SendJSON(w, "pos", "get_tva_rates_error", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, "pos", "get_tva_rates", updated)
}

func (h *POSHandler) ToggleSafetyStockActive(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	var req models.StatusRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorJSON(w, err)
		return
	}

	updated, err := h.service.ToggleSafetyStock(ctx, token, req.Status)
	if err != nil {
		models.SendJSON(w, "pos", "toggle_safety_stock_error", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, "pos", "toggle_safety_stock", models.AvailabilityResponse{
		Status:  "1",
		Updated: updated,
	})
}

func (h *POSHandler) GetDeliveryMen(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	users, err := h.service.GetDeliveryMen(ctx, token)
	if err != nil {
		models.SendJSON(w, "pos", "get_delivery_men_error", map[string]interface{}{"status": "0", "error": err.Error()})
		return
	}

	models.SendJSON(w, "pos", "get_delivery_men", models.DeliveryMenResponse{
		Users: users,
	})
}

func (h *POSHandler) json(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *POSHandler) errorJSON(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "0",
		"error":  err.Error(),
	})
}

func (h *POSHandler) CheckTR(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	code := chi.URLParam(r, "code")

	resp, err := h.service.CheckTR(ctx, token, code)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		models.SendJSON(w, "pos", "check_tr_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "pos", "check_tr", resp)
}

func (h *POSHandler) UpdateMerchantSettings(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	var req models.UpdateMerchantSettingsRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorJSON(w, err)
		return
	}

	if err := h.service.UpdateMerchantSettings(r.Context(), token, &req); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		models.SendJSON(w, "pos", "update_merchant_settings_error", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, "pos", "update_merchant_settings", map[string]string{"status": "ok"})
}

func (h *POSHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	resp, err := h.service.GetMerchantSettings(ctx, token)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		models.SendJSON(w, "pos", "get_settings_error", map[string]string{"status": "-2", "error": err.Error()})
		return
	}

	models.SendJSON(w, "pos", "get_settings", resp)
}
