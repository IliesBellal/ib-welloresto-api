package scannorder

import (
	"encoding/json"
	"errors"
	"net/http"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// statusClientClosedRequest : convention nginx (499) pour une requête dont le
// client est parti avant la réponse. Personne ne lit la réponse ; le code ne
// sert qu'au journal d'accès.
const statusClientClosedRequest = 499

type Handler struct {
	service *Service
}

func NewHandler(s *Service) *Handler {
	return &Handler{service: s}
}

func (h *Handler) GetMerchant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	qr := chi.URLParam(r, "merchant_slug")

	merchantData, err := h.service.GetMerchant(ctx, qr)
	if err != nil {
		if isClientGone(ctx, err) {
			log.Debug("GetMerchant: request canceled by client", zap.String("qr_code", qr))
			w.WriteHeader(statusClientClosedRequest)
			return
		}
		log.Error("service error" + err.Error())
		models.SendJSON(w, http.StatusInternalServerError, "scannorder", "get_merchant", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_merchant", merchantData)
}

// GetSitemap liste les slugs des pages établissement à référencer — consommé
// par le serveur du front ScanNOrder pour générer /sitemap.xml.
func (h *Handler) GetSitemap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	resp, err := h.service.GetSitemap(ctx)
	if err != nil {
		logger.FromContext(ctx).Error("GetSitemap failed", zap.Error(err))
		models.SendErrorJSON(w, "scannorder", "get_sitemap", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_sitemap", resp)
}

func (h *Handler) GetMenu(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	qr := chi.URLParam(r, "merchant_slug")
	orderType := r.URL.Query().Get("order_type")

	log.Info("ScannOrder.GetMenu qr:" + qr + " - order_type: " + orderType)

	resp, err := h.service.GetMenu(ctx, qr, orderType)
	if err != nil {
		if isClientGone(ctx, err) {
			log.Debug("GetMenu: request canceled by client", zap.String("qr_code", qr))
			w.WriteHeader(statusClientClosedRequest)
			return
		}
		if errors.Is(err, ErrMerchantUnavailable) {
			log.Warn("GetMenu: merchant unavailable", zap.String("qr_code", qr))
			models.SendJSON(w, http.StatusNotFound, "scannorder", "get_menu", map[string]string{"error": err.Error()})
			return
		}
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
		models.SendJSON(w, http.StatusBadRequest, "scannorder", "get_pricing_sno", map[string]string{"error": "invalid_body", "message": err.Error()})
		return
	}

	qr := chi.URLParam(r, "merchant_slug")
	req.QRCode = qr
	resp, err := h.service.GetPricingSNO(ctx, &req)
	if err != nil {
		log.Error("SNO pricing failed", zap.Error(err))
		models.SendJSON(w, http.StatusInternalServerError, "scannorder", "get_pricing_sno", map[string]string{"error": err.Error(), "message": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_pricing_sno", resp)
}

func (h *Handler) GetOrderSNO(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	orderIDStr := chi.URLParam(r, "order_id")
	qrCode := chi.URLParam(r, "merchant_slug")

	orders, err := h.service.GetOrderSNO(ctx, qrCode, orderIDStr)
	if errors.Is(err, models.ErrNotFound) {
		models.SendErrorJSON(w, "scannorder", "get_order_sno", err)
		return
	}
	if err != nil {
		log.Error("GetOrderSNO failed", zap.Error(err))
		models.SendJSON(w, http.StatusInternalServerError, "scannorder", "get_order_sno", map[string]string{"error": err.Error(), "message": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_order_sno", orders)
}

func (h *Handler) CancelOrderSNO(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	qr := chi.URLParam(r, "merchant_slug")
	orderIDStr := chi.URLParam(r, "order_id")

	resp, err := h.service.CancelOrderSNO(ctx, qr, orderIDStr)
	if err != nil {
		log.Error("CancelOrderSNO failed", zap.Error(err))
		models.SendErrorJSON(w, "scannorder", "cancel_order_sno", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "cancel_order_sno", resp)
}

func (h *Handler) CreateOrderSNO(w http.ResponseWriter, r *http.Request) {
	var req models.PricingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "scannorder", "create_order_sno", map[string]string{"error": "invalid_body", "message": err.Error()})
		return
	}

	qr := chi.URLParam(r, "merchant_slug")
	req.QRCode = qr

	create_order, err := h.service.CreateOrderSNO(r.Context(), &req)
	if err != nil {
		models.SendErrorJSON(w, "scannorder", "create_order_sno", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "create_order_sno", create_order)
}


// GetFranchise — GET /scannorder/franchises/{franchise_slug} : hub public
// d'une franchise.
func (h *Handler) GetFranchise(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	resp, err := h.service.GetFranchiseHub(ctx, chi.URLParam(r, "franchise_slug"))
	if err != nil {
		log.Error("GetFranchiseHub failed", zap.Error(err))
		models.SendErrorJSON(w, "scannorder", "get_franchise", err)
		return
	}

	if resp.Franchise == nil {
		models.SendJSON(w, http.StatusNotFound, "scannorder", "get_franchise", map[string]string{"error": "franchise_not_found", "message": "Franchise not found"})
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_franchise", resp)
}

func (h *Handler) CheckDeliveryZone(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	// Parse the request body
	var req DeliveryCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("Invalid request body", zap.Error(err))
		models.SendErrorJSON(w, "scannorder", "check_delivery_zone", err)
		return
	}

	// Extract QR code from URL parameter
	qrCode := chi.URLParam(r, "merchant_slug")
	log.Info("CheckDeliveryZone", zap.String("merchant_slug", qrCode), zap.Float64("lat", req.Lat), zap.Float64("lng", req.Lng))

	// Call the service
	resp, err := h.service.CheckDeliveryZone(ctx, qrCode, &req)
	if err != nil {
		log.Error("CheckDeliveryZone service error", zap.Error(err))
		models.SendErrorJSON(w, "scannorder", "check_delivery_zone", err)
		return
	}

	// Determine HTTP status based on delivery zone check
	statusCode := http.StatusOK
	if resp.Status == "out_of_delivery_zone" {
		statusCode = http.StatusUnprocessableEntity // 422
	}

	models.SendJSON(w, statusCode, "scannorder", "check_delivery_zone", resp)
}

// DeliveryHoursPayload : corps de GET/PUT /pos/settings/delivery_hours.
type DeliveryHoursPayload struct {
	DeliveryHours []DeliveryHour `json:"delivery_hours"`
}

// GetDeliveryHours handles GET /pos/settings/delivery_hours (back-office).
func (h *Handler) GetDeliveryHours(w http.ResponseWriter, r *http.Request) {
	hours, err := h.service.GetDeliveryHoursSettings(r.Context())
	if err != nil {
		models.SendErrorJSON(w, "scannorder", "get_delivery_hours", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "scannorder", "get_delivery_hours", DeliveryHoursPayload{DeliveryHours: hours})
}

// ReplaceDeliveryHours handles PUT /pos/settings/delivery_hours (back-office) :
// remplace toute la semaine ; une liste vide supprime la restriction.
func (h *Handler) ReplaceDeliveryHours(w http.ResponseWriter, r *http.Request) {
	var req DeliveryHoursPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "scannorder", "replace_delivery_hours", models.ErrInvalidRequestBody)
		return
	}
	hours, err := h.service.ReplaceDeliveryHoursSettings(r.Context(), req.DeliveryHours)
	if err != nil {
		models.SendErrorJSON(w, "scannorder", "replace_delivery_hours", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "scannorder", "replace_delivery_hours", DeliveryHoursPayload{DeliveryHours: hours})
}

func (h *Handler) GetLoyaltyPrograms(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	qr := chi.URLParam(r, "merchant_slug")
	orderType := r.URL.Query().Get("order_type")

	log.Info("ScannOrder.GetLoyaltyPrograms qr:" + qr + " - order_type: " + orderType)

	resp, err := h.service.GetLoyaltyPrograms(ctx, qr, orderType)
	if err != nil {
		log.Error("GetLoyaltyPrograms error", zap.Error(err))
		models.SendErrorJSON(w, "scannorder", "get_loyalty_programs", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_loyalty_programs", resp)
}

func (h *Handler) GetSlots(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	qr := chi.URLParam(r, "merchant_slug")

	log.Info("ScannOrder.GetSlots qr:" + qr)

	slots, err := h.service.GetSlots(ctx, qr)
	if err != nil {
		log.Error("GetSlots error", zap.Error(err))
		models.SendErrorJSON(w, "scannorder", "get_slots", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_slots", slots)
}

func (h *Handler) GetDiscounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	qr := chi.URLParam(r, "merchant_slug")
	orderType := r.URL.Query().Get("order_type")

	log.Info("ScannOrder.GetDiscounts qr:" + qr + " - order_type: " + orderType)

	resp, err := h.service.GetDiscounts(ctx, qr, orderType)
	if err != nil {
		log.Error("GetDiscounts error", zap.Error(err))
		models.SendErrorJSON(w, "scannorder", "get_discounts", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_discounts", resp)
}

// GetUpsell is DEPRECATED: static is_popular selection, no cart context.
// Kept temporarily in parallel with PostUpsellSNO while the frontend migrates
// to POST /scannorder/{merchant_slug}/upsell (cart-aware, same engine as POS/Kiosk).
// À supprimer une fois le front migré sur POST (voir Phase A résiduelle).
func (h *Handler) GetUpsell(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	qr := chi.URLParam(r, "merchant_slug")
	// order_type optionnel : absent (anciennes versions) → TAKE_AWAY côté service.
	orderType := r.URL.Query().Get("order_type")

	resp, err := h.service.GetUpsell(ctx, qr, orderType)
	if err != nil {
		models.SendErrorJSON(w, "scannorder", "get_upsell", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_upsell", resp)
}

// PostUpsellSNO generates cart-aware upsell suggestions via upsell.Service.GenerateUpsell
// (Apriori/LLM/featured fallback, same engine as POS/Kiosk), enriched with full product
// configuration. Payload reuses models.PricingRequest like GetPricingSNO/CreateOrderSNO.
func (h *Handler) PostUpsellSNO(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req models.PricingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "scannorder", "post_upsell", map[string]string{"error": "invalid_body"})
		return
	}

	qr := chi.URLParam(r, "merchant_slug")
	req.QRCode = qr

	resp, err := h.service.PostUpsell(ctx, &req)
	if err != nil {
		log.Error("PostUpsellSNO failed", zap.Error(err))
		models.SendErrorJSON(w, "scannorder", "post_upsell", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "post_upsell", resp)
}

func (h *Handler) GetProduct(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	qr := chi.URLParam(r, "merchant_slug")
	productID := chi.URLParam(r, "product_id")
	orderType := r.URL.Query().Get("order_type") // Query param: "DELIVERY" or "TAKE_AWAY"

	log.Info("ScannOrder.GetProduct qr:" + qr + " - product_id: " + productID + " - order_type: " + orderType)

	product, err := h.service.GetProduct(ctx, qr, productID, orderType)
	if err != nil {
		log.Error("GetProduct error", zap.Error(err))
		models.SendErrorJSON(w, "scannorder", "get_product", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_product", product)
}
