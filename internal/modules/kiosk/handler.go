package kiosk

import (
	"encoding/json"
	"net/http"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
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

func (h *Handler) EnrollDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req EnrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "kiosk", "enroll_device", models.ErrInvalidRequestBody)
		return
	}

	resp, err := h.service.EnrollDevice(ctx, req, r.RemoteAddr)
	if err != nil {
		log.Warn("kiosk enroll failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "enroll_device", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "enroll_device", resp)
}

func (h *Handler) RefreshDeviceToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req RefreshTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "kiosk", "refresh_device_token", models.ErrInvalidRequestBody)
		return
	}

	resp, err := h.service.RefreshDeviceToken(ctx, req.RefreshToken)
	if err != nil {
		log.Warn("kiosk refresh failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "refresh_device_token", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "refresh_device_token", resp)
}

func (h *Handler) DeviceHeartbeat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	authenticatedKiosk := middleware.GetKiosk(r)
	if authenticatedKiosk == nil {
		models.SendErrorJSON(w, "kiosk", "device_heartbeat", models.ErrKioskDeviceTokenInvalid)
		return
	}

	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "kiosk", "device_heartbeat", models.ErrInvalidRequestBody)
		return
	}

	resp, err := h.service.RecordHeartbeat(ctx, authenticatedKiosk, req, r.RemoteAddr)
	if err != nil {
		log.Warn("kiosk heartbeat failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "device_heartbeat", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "device_heartbeat", resp)
}

// GetKioskMenu handles GET /kiosk/menu — supports conditional requests via
// If-None-Match (the ETag is a hash of the filtered, serialized menu).
func (h *Handler) GetKioskMenu(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	authenticatedKiosk := middleware.GetKiosk(r)
	if authenticatedKiosk == nil {
		models.SendErrorJSON(w, "kiosk", "get_menu", models.ErrKioskDeviceTokenInvalid)
		return
	}

	resp, err := h.service.GetMenu(ctx, authenticatedKiosk.MerchantID)
	if err != nil {
		log.Error("kiosk get menu failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "get_menu", err)
		return
	}

	if match := r.Header.Get("If-None-Match"); match != "" && match == resp.ETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("ETag", resp.ETag)
	models.SendJSON(w, http.StatusOK, "kiosk", "get_menu", resp)
}

func (h *Handler) GetKioskProduct(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	authenticatedKiosk := middleware.GetKiosk(r)
	if authenticatedKiosk == nil {
		models.SendErrorJSON(w, "kiosk", "get_product", models.ErrKioskDeviceTokenInvalid)
		return
	}

	productID := chi.URLParam(r, "product_id")
	if productID == "" {
		models.SendErrorJSON(w, "kiosk", "get_product", models.ErrMissingResourceID)
		return
	}

	resp, err := h.service.GetProduct(ctx, authenticatedKiosk.MerchantID, productID)
	if err != nil {
		log.Warn("kiosk get product failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "get_product", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "get_product", resp)
}

func (h *Handler) GetKioskSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	authenticatedKiosk := middleware.GetKiosk(r)
	if authenticatedKiosk == nil {
		models.SendErrorJSON(w, "kiosk", "get_settings", models.ErrKioskDeviceTokenInvalid)
		return
	}

	resp, err := h.service.GetSettings(ctx, authenticatedKiosk.MerchantID)
	if err != nil {
		log.Error("kiosk get settings failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "get_settings", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "get_settings", resp)
}

func (h *Handler) GetKioskUpsell(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	authenticatedKiosk := middleware.GetKiosk(r)
	if authenticatedKiosk == nil {
		models.SendErrorJSON(w, "kiosk", "get_upsell", models.ErrKioskDeviceTokenInvalid)
		return
	}

	var req KioskUpsellRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "kiosk", "get_upsell", models.ErrInvalidRequestBody)
		return
	}

	resp, err := h.service.GetUpsellSuggestions(ctx, authenticatedKiosk.MerchantID, req.CartProductIDs)
	if err != nil {
		log.Warn("kiosk get upsell failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "get_upsell", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "get_upsell", resp)
}

func (h *Handler) GetKioskPricing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	authenticatedKiosk := middleware.GetKiosk(r)
	if authenticatedKiosk == nil {
		models.SendErrorJSON(w, "kiosk", "get_pricing", models.ErrKioskDeviceTokenInvalid)
		return
	}

	var req KioskPricingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "kiosk", "get_pricing", models.ErrInvalidRequestBody)
		return
	}

	resp, err := h.service.ComputePricing(ctx, authenticatedKiosk.MerchantID, req)
	if err != nil {
		log.Warn("kiosk get pricing failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "get_pricing", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "get_pricing", resp)
}

func (h *Handler) CreateKioskOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	authenticatedKiosk := middleware.GetKiosk(r)
	if authenticatedKiosk == nil {
		models.SendErrorJSON(w, "kiosk", "create_order", models.ErrKioskDeviceTokenInvalid)
		return
	}

	var req CreateKioskOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "kiosk", "create_order", models.ErrInvalidRequestBody)
		return
	}

	resp, err := h.service.CreateKioskOrder(ctx, req, *authenticatedKiosk)
	if err != nil {
		log.Warn("kiosk create order failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "create_order", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "create_order", resp)
}

func (h *Handler) GetKioskOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	authenticatedKiosk := middleware.GetKiosk(r)
	if authenticatedKiosk == nil {
		models.SendErrorJSON(w, "kiosk", "get_order", models.ErrKioskDeviceTokenInvalid)
		return
	}

	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		models.SendErrorJSON(w, "kiosk", "get_order", models.ErrMissingResourceID)
		return
	}

	resp, err := h.service.GetKioskOrder(ctx, orderID, *authenticatedKiosk)
	if err != nil {
		log.Warn("kiosk get order failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "get_order", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "get_order", resp)
}

func (h *Handler) CancelKioskOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	authenticatedKiosk := middleware.GetKiosk(r)
	if authenticatedKiosk == nil {
		models.SendErrorJSON(w, "kiosk", "cancel_order", models.ErrKioskDeviceTokenInvalid)
		return
	}

	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		models.SendErrorJSON(w, "kiosk", "cancel_order", models.ErrMissingResourceID)
		return
	}

	if err := h.service.CancelKioskOrder(ctx, orderID, *authenticatedKiosk); err != nil {
		log.Warn("kiosk cancel order failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "cancel_order", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "cancel_order", map[string]string{"status": "cancelled"})
}

func (h *Handler) ConfirmCounterPayment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	authenticatedKiosk := middleware.GetKiosk(r)
	if authenticatedKiosk == nil {
		models.SendErrorJSON(w, "kiosk", "confirm_counter_payment", models.ErrKioskDeviceTokenInvalid)
		return
	}

	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		models.SendErrorJSON(w, "kiosk", "confirm_counter_payment", models.ErrMissingResourceID)
		return
	}

	resp, err := h.service.ConfirmCounterPayment(ctx, orderID, *authenticatedKiosk)
	if err != nil {
		log.Warn("kiosk confirm counter payment failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "confirm_counter_payment", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "confirm_counter_payment", resp)
}
