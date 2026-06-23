package kiosk

import (
	"encoding/json"
	"errors"
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

// VerifyAdminPin handles POST /kiosk/auth/verify-admin-pin — la borne est
// déjà authentifiée (KioskAuth) ; ce PIN ne fait que déverrouiller l'écran
// admin local. Rate-limité côté service (5 tentatives, 30s de lockout).
func (h *Handler) VerifyAdminPin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	authenticatedKiosk := middleware.GetKiosk(r)
	if authenticatedKiosk == nil {
		models.SendErrorJSON(w, "kiosk", "verify_admin_pin", models.ErrKioskDeviceTokenInvalid)
		return
	}

	var req VerifyAdminPinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "kiosk", "verify_admin_pin", models.ErrInvalidRequestBody)
		return
	}

	resp, err := h.service.VerifyAdminPin(ctx, authenticatedKiosk, req.Pin)
	if err != nil {
		var lockoutErr *AdminPinLockoutError
		if errors.As(err, &lockoutErr) {
			models.SendJSON(w, http.StatusTooManyRequests, "kiosk", "verify_admin_pin", map[string]interface{}{
				"error":         "kiosk_admin_pin_locked",
				"delay_seconds": lockoutErr.DelaySeconds,
			})
			return
		}
		log.Warn("kiosk verify admin pin failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "verify_admin_pin", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "verify_admin_pin", resp)
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

	// order_type ("IN"/"TAKE_AWAY") suit le même vocabulaire que scannorder
	// (GET /{merchant_slug}/menu?order_type=...) — optionnel : un kiosk qui n'a
	// pas encore demandé le mode au client (écran d'accueil) reçoit le menu au
	// prix "IN" par défaut.
	orderType := r.URL.Query().Get("order_type")
	if orderType == "" {
		orderType = models.OrderTypeIn
	}

	resp, err := h.service.GetMenu(ctx, authenticatedKiosk.MerchantID, orderType)
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

	orderType := r.URL.Query().Get("order_type")
	if orderType == "" {
		orderType = models.OrderTypeIn
	}

	resp, err := h.service.GetProduct(ctx, authenticatedKiosk.MerchantID, productID, orderType)
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

func (h *Handler) GetKioskDiscounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	authenticatedKiosk := middleware.GetKiosk(r)
	if authenticatedKiosk == nil {
		models.SendErrorJSON(w, "kiosk", "get_discounts", models.ErrKioskDeviceTokenInvalid)
		return
	}

	resp, err := h.service.GetDiscounts(ctx, authenticatedKiosk.MerchantID)
	if err != nil {
		log.Error("kiosk get discounts failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "get_discounts", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "get_discounts", resp)
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

// kioskFulfillmentTypeOf lit le fulfillment_type envoyé par la borne dans le
// body (champ déjà porté par models.OrderRequest, voir
// internal/models/create_order_models.go), sans struct kiosk dédiée.
func kioskFulfillmentTypeOf(order *models.OrderRequest) string {
	if order == nil || order.FulfillmentType == nil {
		return ""
	}
	return *order.FulfillmentType
}

// GetKioskPricing décode directement models.PricingRequest (même contrat que
// scannorder.GetPricingSNO) — seule traduction kiosk-spécifique : le
// fulfillment_type du body (DINE_IN/TAKE_AWAY) est mappé vers order_type
// avant d'appeler le service, voir docs/KIOSK_DECISIONS.md.
func (h *Handler) GetKioskPricing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	authenticatedKiosk := middleware.GetKiosk(r)
	if authenticatedKiosk == nil {
		models.SendErrorJSON(w, "kiosk", "get_pricing", models.ErrKioskDeviceTokenInvalid)
		return
	}

	var req models.PricingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "kiosk", "get_pricing", models.ErrInvalidRequestBody)
		return
	}
	if req.Order == nil {
		models.SendErrorJSON(w, "kiosk", "get_pricing", models.ErrInvalidInput)
		return
	}

	orderType, err := kioskFulfillmentToOrderType(kioskFulfillmentTypeOf(req.Order))
	if err != nil {
		models.SendErrorJSON(w, "kiosk", "get_pricing", err)
		return
	}
	req.Order.OrderType = orderType
	req.MerchantID = authenticatedKiosk.MerchantID

	resp, err := h.service.ComputePricing(ctx, &req)
	if err != nil {
		log.Warn("kiosk get pricing failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "get_pricing", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "get_pricing", resp)
}

// CreateKioskOrder décode directement models.RequestObject (même contrat que
// scannorder.CreateOrderSNO). Traductions kiosk-spécifiques faites ici, avant
// d'appeler le service : fulfillment_type → order_type, et la clé
// d'idempotence (sans équivalent dans les structs partagées) lue depuis le
// header HTTP "Idempotency-Key" plutôt que depuis le body.
func (h *Handler) CreateKioskOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	authenticatedKiosk := middleware.GetKiosk(r)
	if authenticatedKiosk == nil {
		models.SendErrorJSON(w, "kiosk", "create_order", models.ErrKioskDeviceTokenInvalid)
		return
	}

	var req models.RequestObject
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "kiosk", "create_order", models.ErrInvalidRequestBody)
		return
	}

	orderType, err := kioskFulfillmentToOrderType(kioskFulfillmentTypeOf(&req.Order))
	if err != nil {
		models.SendErrorJSON(w, "kiosk", "create_order", err)
		return
	}
	req.Order.OrderType = orderType
	req.MerchantID = authenticatedKiosk.MerchantID

	idempotencyKey := r.Header.Get("Idempotency-Key")

	resp, err := h.service.CreateOrder(ctx, &req, *authenticatedKiosk, idempotencyKey)
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

// ReportUnavailable handles POST /kiosk/status/unavailable — la borne
// signale elle-même un problème (connection_lost/app_error/manual). Diffuse
// kiosk_unavailable sur le hub WebSocket du merchant.
func (h *Handler) ReportUnavailable(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	authenticatedKiosk := middleware.GetKiosk(r)
	if authenticatedKiosk == nil {
		models.SendErrorJSON(w, "kiosk", "report_unavailable", models.ErrKioskDeviceTokenInvalid)
		return
	}

	var req ReportUnavailableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "kiosk", "report_unavailable", models.ErrInvalidRequestBody)
		return
	}

	if err := h.service.ReportUnavailable(ctx, authenticatedKiosk, req.Reason); err != nil {
		log.Warn("kiosk report unavailable failed", zap.Error(err))
		models.SendErrorJSON(w, "kiosk", "report_unavailable", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "kiosk", "report_unavailable", map[string]string{"status": "ok"})
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
