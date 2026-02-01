package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"welloresto-api/internal/models"

	"welloresto-api/internal/services"

	"github.com/go-chi/chi/v5"
)

type POSHandler struct {
	service *services.POSService
}

func NewPOSHandler(s *services.POSService) *POSHandler {
	return &POSHandler{service: s}
}

func (h *POSHandler) GetPOSStatus(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	resp, err := h.service.GetPOSStatus(r.Context(), token)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   10,
			"data": map[string]string{"error": err.Error()},
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":   10,
		"data": map[string]interface{}{"pos_status": resp},
	})
}

func (h *POSHandler) UpdatePOSStatus(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
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
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   10,
			"data": map[string]string{"error": err.Error()},
		})
		return
	}

	// Return same as GET
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":   10,
		"data": map[string]interface{}{"pos_status": resp},
	})
}

func (h *POSHandler) GetDeletionReasons(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	obj := chi.URLParam(r, "object")

	reasons, err := h.service.GetDeletionReasons(ctx, obj)
	if err != nil {
		h.errorJSON(w, err)
		return
	}

	h.json(w, models.DeletionReasonResponse{
		Status:          "1",
		DeletionReasons: reasons,
	}, 200)
}

func (h *POSHandler) ToggleScanNOrder(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
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
		h.errorJSON(w, err)
		return
	}

	h.json(w, models.AvailabilityResponse{
		Status:  "1",
		Updated: updated,
	}, 200)
}

func (h *POSHandler) ToggleProductionPaidOnly(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
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
		h.errorJSON(w, err)
		return
	}

	h.json(w, models.AvailabilityResponse{
		Status:  "1",
		Updated: updated,
	}, 200)
}

func (h *POSHandler) ToggleSafetyStockActive(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
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
		h.errorJSON(w, err)
		return
	}

	h.json(w, models.AvailabilityResponse{
		Status:  "1",
		Updated: updated,
	}, 200)
}

func (h *POSHandler) GetDeliveryMen(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
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
		h.errorJSON(w, err)
		return
	}

	h.json(w, models.DeliveryMenResponse{
		Users: users,
	}, 200)
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
	ctx := r.Context()
	token := extractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}
	code := chi.URLParam(r, "code")

	resp, err := h.service.CheckTR(ctx, token, code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (h *POSHandler) UpdateMerchantSettings(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
	})
}

func (h *POSHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	token := extractToken(r)
	if strings.TrimSpace(token) == "" {
		http.Error(w, `{"status":"-1","error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	resp, err := h.service.GetMerchantSettings(ctx, token)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"status":"-2","error":"%s"}`, err.Error()), 500)
		return
	}

	json.NewEncoder(w).Encode(resp)
}
