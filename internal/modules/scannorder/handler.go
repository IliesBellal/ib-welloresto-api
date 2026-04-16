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

	merchantData, err := h.service.GetMerchant(ctx, qr)
	if err != nil {
		log.Error("service error" + err.Error())
		models.SendJSON(w, http.StatusInternalServerError, "scannorder", "get_merchant", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_merchant", merchantData)
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
		models.SendJSON(w, http.StatusInternalServerError, "scannorder", "get_menu", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_menu", resp)
}

func (h *Handler) GetPricingSNO(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req models.PricingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "scannorder", "get_pricing_sno", map[string]string{"error": "invalid_body"})
		return
	}

	qr := chi.URLParam(r, "qr_code")
	req.QRCode = qr
	resp, err := h.service.GetPricingSNO(ctx, &req)
	if err != nil {
		log.Error("SNO pricing failed", zap.Error(err))
		models.SendJSON(w, http.StatusInternalServerError, "scannorder", "get_pricing_sno", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_pricing_sno", resp)
}

func (h *Handler) GetOrderSNO(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	orderIDStr := chi.URLParam(r, "order_id")
	qrCode := chi.URLParam(r, "qr_code")

	orders, err := h.service.GetOrderSNO(ctx, qrCode, orderIDStr)
	if err != nil {
		log.Error("GetOrderSNO failed", zap.Error(err))
		models.SendJSON(w, http.StatusInternalServerError, "scannorder", "get_order_sno", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_order_sno", orders)
}

func (h *Handler) CancelOrderSNO(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	qr := chi.URLParam(r, "qr_code")
	orderIDStr := chi.URLParam(r, "order_id")

	resp, err := h.service.CancelOrderSNO(ctx, qr, orderIDStr)
	if err != nil {
		log.Error("CancelOrderSNO failed", zap.Error(err))
		models.SendJSON(w, http.StatusInternalServerError, "scannorder", "cancel_order_sno", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "cancel_order_sno", resp)
}

func (h *Handler) CreateOrderSNO(w http.ResponseWriter, r *http.Request) {
	var req models.PricingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "scannorder", "create_order_sno", map[string]string{"error": "invalid_body"})
		return
	}

	qr := chi.URLParam(r, "qr_code")
	req.QRCode = qr

	create_order, err := h.service.CreateOrderSNO(r.Context(), &req)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "scannorder", "create_order_sno", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "create_order_sno", create_order)
}

func (h *Handler) GetBrand(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	slug := chi.URLParam(r, "slug")
	latStr := r.URL.Query().Get("lat")
	lngStr := r.URL.Query().Get("lng")

	resp, err := h.service.GetBrand(ctx, slug, latStr, lngStr)
	if err != nil {
		log.Error("GetBrand failed", zap.Error(err))
		models.SendJSON(w, http.StatusInternalServerError, "scannorder", "get_brand", map[string]string{"error": err.Error()})
		return
	}

	if resp.Brand == nil {
		models.SendJSON(w, http.StatusNotFound, "scannorder", "get_brand", map[string]string{"error": "brand_not_found"})
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_brand", resp)
}

func (h *Handler) CheckDeliveryZone(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	// Parse the request body
	var req DeliveryCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("Invalid request body", zap.Error(err))
		models.SendJSON(w, http.StatusBadRequest, "scannorder", "check_delivery_zone", map[string]string{"error": "invalid_body"})
		return
	}

	// Extract QR code from URL parameter
	qrCode := chi.URLParam(r, "qr_code")
	log.Info("CheckDeliveryZone", zap.String("qr_code", qrCode), zap.Float64("lat", req.Lat), zap.Float64("lng", req.Lng))

	// Call the service
	resp, err := h.service.CheckDeliveryZone(ctx, qrCode, &req)
	if err != nil {
		log.Error("CheckDeliveryZone service error", zap.Error(err))
		models.SendJSON(w, http.StatusInternalServerError, "scannorder", "check_delivery_zone", map[string]string{"error": err.Error()})
		return
	}

	// Determine HTTP status based on delivery zone check
	statusCode := http.StatusOK
	if resp.Status == "out_of_delivery_zone" {
		statusCode = http.StatusUnprocessableEntity // 422
	}

	models.SendJSON(w, statusCode, "scannorder", "check_delivery_zone", resp)
}

func (h *Handler) GetLoyaltyPrograms(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	qr := chi.URLParam(r, "qr_code")
	deliveryType := r.URL.Query().Get("type")

	log.Info("ScannOrder.GetLoyaltyPrograms qr:" + qr + " - type: " + deliveryType)

	resp, err := h.service.GetLoyaltyPrograms(ctx, qr, deliveryType)
	if err != nil {
		log.Error("GetLoyaltyPrograms error", zap.Error(err))
		models.SendJSON(w, http.StatusInternalServerError, "scannorder", "get_loyalty_programs", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_loyalty_programs", resp)
}

func (h *Handler) GetDiscounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	qr := chi.URLParam(r, "qr_code")
	orderType := r.URL.Query().Get("order_type")

	log.Info("ScannOrder.GetDiscounts qr:" + qr + " - type: " + orderType)

	resp, err := h.service.GetDiscounts(ctx, qr, orderType)
	if err != nil {
		log.Error("GetDiscounts error", zap.Error(err))
		models.SendJSON(w, http.StatusInternalServerError, "scannorder", "get_discounts", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_discounts", resp)
}

func (h *Handler) GetUpsell(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	qr := chi.URLParam(r, "qr_code")

	resp, err := h.service.GetUpsell(ctx, qr)
	if err != nil {
		models.SendJSON(w, http.StatusInternalServerError, "scannorder", "get_upsell", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_upsell", resp)
}
