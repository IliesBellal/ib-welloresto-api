package scannorder

import (
	"encoding/json"
	"net/http"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type Handler struct {
	service *Service
}

func NewHandler(s *Service) *Handler {
	return &Handler{service: s}
}

func (h *Handler) GetMerchant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	qr := chi.URLParam(r, "qr_code")
	log.Info("ScannOrder.GetMerchant called - " + "qr_code: " + qr)

	merchantData, err := h.service.GetMerchant(ctx, qr)
	if err != nil {
		log.Error("service error" + err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := models.HandlerDefaultResponse{
		ID:   "scannorder.merchant",
		Data: merchantData,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) GetMenu(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	qr := chi.URLParam(r, "qr_code")
	deliveryType := r.URL.Query().Get("type")

	log.Info("ScannOrder.GetMenu qr:" + qr + " - type: " + deliveryType)

	resp, err := h.service.GetMenu(ctx, qr, deliveryType)
	if err != nil {
		log.Error("GetMenu error " + err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) GetPricingSNO(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req models.PricingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	qr := chi.URLParam(r, "qr_code")
	req.QRCode = qr
	resp, err := h.service.GetPricingSNO(ctx, &req)
	if err != nil {
		log.Error("SNO pricing failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) GetOrderSNO(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	orderIDStr := chi.URLParam(r, "order_id")
	qrCode := chi.URLParam(r, "qr_code")

	orders, err := h.service.GetOrderSNO(ctx, qrCode, orderIDStr)
	if err != nil {
		log.Error("GetOrderSNO failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := models.HandlerDefaultResponse{
		ID:   "scannorder.order",
		Data: orders,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) CancelOrderSNO(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	qr := chi.URLParam(r, "qr_code")
	orderIDStr := chi.URLParam(r, "order_id")

	resp, err := h.service.CancelOrderSNO(ctx, qr, orderIDStr)
	if err != nil {
		log.Error("CancelOrderSNO failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) CreateOrderSNO(w http.ResponseWriter, r *http.Request) {
	var req models.PricingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	qr := chi.URLParam(r, "qr_code")
	req.QRCode = qr

	create_order, err := h.service.CreateOrderSNO(r.Context(), &req)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	resp := models.HandlerDefaultResponse{
		ID:   "scannorder.order.create",
		Data: create_order,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
