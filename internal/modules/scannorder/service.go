package scannorder

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"
	"welloresto-api/internal/config"
	"welloresto-api/internal/infrastructure/redis"
	stripeclient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/delivery_sessions"
	"welloresto-api/internal/modules/menu"
	"welloresto-api/internal/modules/order_life_cycle"
	"welloresto-api/internal/modules/orders"

	"go.uber.org/zap"
)

type Service struct {
	repo                   *Repository
	menu                   *menu.MenuService
	orderingService        *orders.OrdersService
	orderLifeCycleSvc      *order_life_cycle.OrdersLifeCycleService
	deliverySessionService delivery_sessions.DeliverySessionsService
	StripeManager          *stripeclient.StripeManager
	cfg                    config.ScanNOrderConfig
	redis                  *redis.Client
}

func NewService(config config.ScanNOrderConfig, r *Repository, m *menu.MenuService, o *orders.OrdersService, manager *stripeclient.StripeManager, redis *redis.Client, orderLifeCycleSvc *order_life_cycle.OrdersLifeCycleService) *Service {
	return &Service{cfg: config, repo: r, menu: m, orderingService: o, StripeManager: manager, redis: redis, orderLifeCycleSvc: orderLifeCycleSvc}
}

func (s *Service) GetMerchant(ctx context.Context, qr string) (*MerchantResponse, error) {
	// Si Redis n'est pas configuré, on court direct à la BDD
	if s.redis == nil {
		return s.computeGetMerchant(ctx, qr)
	}

	log := logger.FromContext(ctx)
	cacheKey := models.ScannorderMerchant + qr

	// --- ÉTAPE 1 : Chercher dans Redis ---
	cached, found := s.redis.Get(ctx, cacheKey)

	if found {
		// Cache hit !
		var merchant MerchantResponse
		if err := json.Unmarshal([]byte(cached), &merchant); err == nil {
			log.Info("🧠🏪 Merchant found in Redis cache 🏪🧠")
			return &merchant, nil
		}
	}

	log.Info("🧠🚫 Merchant not found in Redis cache 🚫🧠")

	// --- ÉTAPE 2 : Appel BDD (la logique lourde) ---
	merchant, err := s.computeGetMerchant(ctx, qr)
	if err != nil {
		return nil, err // Si la BDD échoue, là c'est un vrai problème
	}

	if merchant == nil {
		return nil, nil
	}

	// --- ÉTAPE 3 : Stocker dans Redis pour la prochaine fois ---
	serialized, err := json.Marshal(merchant)
	if err == nil {
		// Utilise un TTL raisonnable (ex: 24h car un merchant change peu souvent)
		if saved := s.redis.Set(ctx, cacheKey, string(serialized), models.ScannorderMerchantTTL); !saved {
			log.Warn("Warning Redis Set (Merchant): " + err.Error())
		} else {
			log.Info("🧠📌 Merchant saved in Redis cache 📌🧠")
		}
	}

	return merchant, nil
}

func (s *Service) computeGetMerchant(ctx context.Context, qr string) (*MerchantResponse, error) {
	row, err := s.repo.GetMerchantByQR(ctx, qr)
	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
		return &MerchantResponse{Status: "no_merchant_found"}, nil
	}

	// Expiration QR (2h)
	if row.CreationDate != nil {
		creationTime := time.Unix(*row.CreationDate, 0)

		if time.Since(creationTime) > 2*time.Hour {
			return &MerchantResponse{
				Status: "qr_code_expired",
			}, nil
		}
	}

	status, err := s.GetMerchantStatus(ctx, row.MerchantID)
	if err != nil {
		return nil, err
	}
	// 1. Calculer le temps de préparation effectif
	prepMinutes := s.GetEffectivePrepMinutes(ctx, row)

	resp := &MerchantResponse{
		Status: "success",
		Merchant: &MerchantData{
			MerchantID:      row.MerchantID,
			BusinessName:    row.FullName,
			Currency:        row.Currency,
			Phone:           *row.Phone,
			Status:          status,
			PreparationTime: prepMinutes,

			OrderTypes: OrderTypes{
				TakeawayEnabled:   row.TakeawayEnabled,
				TakeawayAvailable: row.TakeawayAvailable,
				DeliveryEnabled:   row.DeliveryEnabled,
				DeliveryAvailable: row.DeliveryAvailable,
				InEnabled:         row.InEnabled,
				InAvailable:       row.InAvailable,
			},

			PaymentTypes: PaymentTypes{
				Cash:   false,
				Online: true,
			},
			AdvanceOrdersEnabled: row.EnableAdvanceOrders,
		},
	}
	// Mapping des sous-structures refactorisées
	resp.Merchant.Address = Address{
		Address: row.Address,
		Lat:     row.Lat,
		Lng:     row.Lng,
	}

	resp.Merchant.Design = MerchantDesign{
		PrimaryColor: row.PrimaryColor,
		TextColor:    row.TextColor,
		LogoURL:      row.LogoURL,
		BannerURL:    row.BannerURL,
	}

	resp.Merchant.Fee = MerchantFees{
		DeliveryFees:      row.DeliveryFees,
		DeliveryFeesLimit: row.DeliveryFeesLimit,
	}
	resp.Merchant.MinimumOrderAmount = row.MinimumCartForDeliveryOrder

	resp.Merchant.QRCode.MenuOnly = row.MenuOnly
	resp.Merchant.QRCode.UserID = row.UserID
	resp.Merchant.QRCode.LastWaiterCall = row.LastWaiterCall
	resp.Merchant.QRCode.OrderID = row.OrderID
	resp.Merchant.QRCode.LocationID = row.LocationID
	resp.Merchant.QRCode.LocationName = row.LocationName

	return resp, nil
}

func (s *Service) GetEffectivePrepMinutes(ctx context.Context, row *models.MerchantRow) int {
	if row.PrepTimeMode == "MANUAL" {
		return row.PrepTime
	}

	// Mode AUTO : on utilise ta logique de procédure stockée
	// Note : On adapte ComputeEstimatedReady pour obtenir juste le délai
	estimatedReadyStr, err := s.orderLifeCycleSvc.ComputeEstimatedReady(ctx, row.MerchantID)
	if err != nil || estimatedReadyStr == "" {
		return row.PrepTime // Fallback sur le temps manuel si l'auto échoue
	}

	// Calcul de la différence entre "maintenant" et "EstimatedReady"
	readyTime, err := time.Parse("2006-01-02 15:04:05", estimatedReadyStr)
	if err != nil {
		return row.PrepTime
	}

	diff := time.Until(readyTime)
	if diff < 0 {
		return 0
	}

	return int(diff.Minutes())
}

func (s *Service) GetMenu(ctx context.Context, qr string, deliveryType string) (*MenuResponse, error) {
	// Si Redis est absent, direct BDD
	if s.redis == nil {
		return s.ComputeGetMenu(ctx, qr, deliveryType)
	}

	log := logger.FromContext(ctx)
	// On combine QR et deliveryType dans la clé pour l'unicité
	cacheKey := fmt.Sprintf("%s%s:%s", models.ScannorderMerchantMenu, qr, deliveryType)

	// --- ÉTAPE 1 : Chercher dans Redis ---
	cached, found := s.redis.Get(ctx, cacheKey)

	if found {
		var menu MenuResponse
		if err := json.Unmarshal([]byte(cached), &menu); err == nil {
			log.Info(fmt.Sprintf("🧠📖 Menu (%s) found in Redis cache 📖🧠", deliveryType))
			return &menu, nil
		}
	}

	log.Info(fmt.Sprintf("🧠🚫 Menu (%s) not found in Redis cache 🚫🧠", deliveryType))

	// --- ÉTAPE 2 : Appel BDD (Calcul lourd) ---
	menu, err := s.ComputeGetMenu(ctx, qr, deliveryType)
	if err != nil {
		return nil, err
	}

	if menu == nil {
		return nil, nil
	}

	// --- ÉTAPE 3 : Stocker dans Redis ---
	serialized, err := json.Marshal(menu)
	if err == nil {
		// Un TTL de 1h ou 2h est généralement un bon compromis pour un menu
		if saved := s.redis.Set(ctx, cacheKey, string(serialized), models.ScannorderMerchantMenuTTL); !saved {
			log.Warn("Warning Redis Set (Menu): " + err.Error())
		} else {
			log.Info("🧠📌 Menu saved in Redis cache 📌🧠")
		}
	}

	return menu, nil
}

func (s *Service) ComputeGetMenu(ctx context.Context, qr string, deliveryType string) (*MenuResponse, error) {

	merchantID, tz, err := s.repo.GetMerchantIDAndTZFromQR(ctx, qr)
	if err != nil || merchantID == "" {
		return &MenuResponse{Status: "-1", Error: "Merchant not found"}, nil
	}

	loc, _ := time.LoadLocation(tz)
	now := time.Now().In(loc)
	dow := int(now.Weekday())
	if dow == 0 {
		dow = 7
	}

	rawMenu, err := s.menu.GetMenuFromMerchantIdWithMarketing(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	menu := &MenuData{
		OrderType:    deliveryType,
		ProductTypes: rawMenu.ProductsTypes,
	}

	filtered := []models.ProductCategory{}

	for _, pt := range menu.ProductTypes {
		products := pt.Products
		finalProducts := []models.ProductEntry{}
		var toAdd []models.ProductEntry

		// --- ÉTAPE 1 : Sélection et extraction (SANS nettoyage) ---
		for _, p := range products {
			// On vérifie si le produit principal doit être affiché tel quel
			isGroup := p.IsProductGroup != nil && *p.IsProductGroup
			isAvailable := p.IsAvailableOnSNO != nil && *p.IsAvailableOnSNO

			if !isGroup && isAvailable {
				finalProducts = append(finalProducts, p)
			} else {
				// Si c'est un groupe non disponible ou un produit simple avec des sous-produits,
				// on récupère les enfants pour les traiter plus tard
				if len(p.SubProducts) > 0 {
					toAdd = append(toAdd, p.SubProducts...)
				}
			}
		}

		// On traite les sous-produits extraits
		for _, sp := range toAdd {
			// On ne garde le sous-produit que s'il est disponible
			if sp.IsAvailableOnSNO != nil && *sp.IsAvailableOnSNO {
				finalProducts = append(finalProducts, sp)
			}
		}

		// --- ÉTAPE 2 : Nettoyage final ---
		// Maintenant que finalProducts contient exactement ce qu'on veut renvoyer,
		// on peut nettoyer sans crainte de casser la logique de filtrage.
		for i := range finalProducts {
			s.cleanProductForSNO(&finalProducts[i], deliveryType)
		}

		// Si on a des produits, on ajoute la catégorie au menu filtré
		if len(finalProducts) > 0 {
			pt.Products = finalProducts
			filtered = append(filtered, pt)
		}
	}

	menu.ProductTypes = filtered
	menu.LoyaltyPrograms, _ = s.repo.GetLoyaltyPrograms(ctx, merchantID, deliveryType)
	menu.Discounts, _ = s.repo.GetDiscounts(ctx, merchantID, deliveryType, dow)

	return &MenuResponse{
		Status: "1",
		Menu:   menu,
	}, nil
}

func (s *Service) GetMerchantStatus(ctx context.Context, merchantID string) (*MerchantStatus, error) {

	// On récupère timezone pour reproduire PHP
	_, tz, err := s.repo.GetMerchantIDAndTZFromMerchantID(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	loc, _ := time.LoadLocation(tz)
	now := time.Now().In(loc)

	dow := int(now.Weekday())
	currentTime := now.Format("15:04:05")

	return s.repo.GetMerchantStatus(ctx, merchantID, dow, currentTime)
}

func (s *Service) GetBrand(ctx context.Context, slug, latStr, lngStr string) (*BrandResponse, error) {
	var lat, lng *float64

	if latStr != "" && lngStr != "" {
		parsedLat, err1 := strconv.ParseFloat(latStr, 64)
		parsedLng, err2 := strconv.ParseFloat(lngStr, 64)
		if err1 == nil && err2 == nil {
			lat = &parsedLat
			lng = &parsedLng
		}
	}

	brand, merchantRows, err := s.repo.GetMerchantsByBrandSlug(ctx, slug, lat, lng)
	if err != nil {
		return nil, err
	}
	if brand == nil {
		return &BrandResponse{Status: "not_found"}, nil
	}

	summaries := make([]MerchantSummary, 0, len(merchantRows))
	for _, row := range merchantRows {
		status, err := s.GetMerchantStatus(ctx, row.MerchantID)
		isOpen := false
		if err == nil {
			isOpen = status.IsOpen
		}

		merchantRow := &models.MerchantRow{
			MerchantID:   row.MerchantID,
			Timezone:     row.Timezone,
			PrepTimeMode: row.PrepTimeMode,
			PrepTime:     row.PrepTime,
		}
		prepMinutes := s.GetEffectivePrepMinutes(ctx, merchantRow)

		summaries = append(summaries, MerchantSummary{
			MerchantID:      row.MerchantID,
			BusinessName:    row.FullName,
			IsOpen:          isOpen,
			PreparationTime: prepMinutes,
			DistanceKm:      row.DistanceKm,
			Address: Address{
				Address: row.Address,
				Lat:     row.Lat,
				Lng:     row.Lng,
			},
			OrderTypes: OrderTypes{
				TakeawayEnabled:   row.TakeawayEnabled,
				TakeawayAvailable: row.TakeawayAvailable,
				DeliveryEnabled:   row.DeliveryEnabled,
				DeliveryAvailable: row.DeliveryAvailable,
				InEnabled:         row.InEnabled,
				InAvailable:       row.InAvailable,
			},
			URL: s.cfg.SNORedirectBaseURL + "/restaurant/" + row.Slug,
		})
	}

	return &BrandResponse{
		Status:    "success",
		Brand:     brand,
		Merchants: summaries,
	}, nil
}

func (s *Service) GetPricingSNO(ctx context.Context, req *models.PricingRequest) (*models.PricingResponse, error) {

	// 🔹 1. Récupérer merchant via QR
	merchant, err := s.repo.GetMerchantByQR(ctx, req.QRCode) // déjà fait dans endpoint précédent
	if err != nil || merchant == nil {
		return &models.PricingResponse{
			Status: "qr_code_expired",
		}, nil
	}

	// 🔹 2. Delivery zone check
	if req.Order.OrderType == "DELIVERY" &&
		req.Order.Customer != nil {

		inZone := s.CustomerInDeliveryZone(*merchant, *req.Order.Customer) // placeholder
		req.IsInDeliveryZone = inZone.InZone
	}

	// 🔹 3. Enrich customer
	if req.Order.Customer != nil {
		req.Order.Customer.MerchantID = &merchant.MerchantID
		customer, _ := s.repo.GetCustomerByPhone(ctx, *req.Order.Customer) // placeholder
		req.Order.Customer = customer
	}

	// 🔹 4. 🔒 SECURITY: Validate and clean prices before transmission
	if err := s.validateAndCleanPricingPayload(ctx, req, merchant); err != nil {
		log := logger.FromContext(ctx)
		log.Error("Price validation failed - potential fraud attempt", zap.Error(err))
		return &models.PricingResponse{
			Status: "pricing_validation_failed",
		}, nil
	}

	// 🔹 5. Timezone logic (IDENTIQUE PHP)
	loc, _ := time.LoadLocation(merchant.Timezone)
	now := time.Now().In(loc)

	req.MerchantID = merchant.MerchantID
	req.IsSNO = true
	// 1 = lundi, ..., 7 = dimanche (1-7 standard)
	req.DayOfWeek = int(now.Weekday())
	req.Time = now.Format("2006-01-02 15:04:05")

	pricing, err := s.orderingService.ComputePricing(ctx, req)

	pricing.IsOrderable = pricing.OrderRequest.IsOrderable
	if pricing.OrderRequest.Order != nil && pricing.OrderRequest.Order.OrderType == "DELIVERY" {
		pricing.IsOrderable = pricing.OrderRequest.IsOrderable && pricing.OrderRequest.IsInDeliveryZone
		if !pricing.OrderRequest.IsInDeliveryZone {
			pricing.NotOrderableReason = "out_of_delivery_zone"
		}
	}
	pricing.OrderRequest.IsSNO = true
	pricing.OrderRequest.Order.IsPaid = false

	// 🔹 6. Appel module ORDERING (prices now sanitized)
	return pricing, err
}

// validateAndCleanPricingPayload ensures all prices come from the database
// This is a critical security function that prevents client-side price manipulation
func (s *Service) validateAndCleanPricingPayload(ctx context.Context, req *models.PricingRequest, merchant *models.MerchantRow) error {
	log := logger.FromContext(ctx)

	if req.Order == nil || len(req.Order.Products) == 0 {
		return nil
	}

	// --- STEP 1: Collect all product and option IDs from payload ---
	productIDs := make([]string, 0)
	optionIDs := make(map[string]bool)

	for _, product := range req.Order.Products {
		productIDs = append(productIDs, product.ProductID)

		// Collect option IDs from configuration
		if product.Config != nil && product.Config.Attributes != nil {
			for _, attr := range product.Config.Attributes {
				for _, opt := range attr.Options {
					optionIDs[opt.ID] = true
				}
			}
		}
	}

	// --- STEP 2: Fetch official prices from database ---
	officialProductPrices, err := s.repo.GetProductPricesForSNO(ctx, merchant.MerchantID, productIDs)
	if err != nil {
		log.Error("Failed to fetch official product prices", zap.Error(err))
		return fmt.Errorf("pricing_fetch_failed: %w", err)
	}

	// Validate all products exist in database
	for _, productID := range productIDs {
		if _, exists := officialProductPrices[productID]; !exists {
			log.Warn("SECURITY: Client sent invalid product ID",
				zap.String("product_id", productID),
				zap.String("merchant_id", merchant.MerchantID),
			)
			return fmt.Errorf("invalid_product_id: %s", productID)
		}
	}

	// Options (if any)
	optIDList := make([]string, 0, len(optionIDs))
	for id := range optionIDs {
		optIDList = append(optIDList, id)
	}

	officialOptionPrices := make(map[string]int)
	if len(optIDList) > 0 {
		officialOptionPrices, err = s.repo.GetConfigurationOptionPricesForSNO(ctx, optIDList)
		if err != nil {
			log.Error("Failed to fetch official option prices", zap.Error(err))
			return fmt.Errorf("option_pricing_fetch_failed: %w", err)
		}

		// Validate all options exist in database
		for _, optionID := range optIDList {
			if _, exists := officialOptionPrices[optionID]; !exists {
				log.Warn("SECURITY: Client sent invalid configuration option ID",
					zap.String("option_id", optionID),
					zap.String("merchant_id", merchant.MerchantID),
				)
				return fmt.Errorf("invalid_option_id: %s", optionID)
			}
		}
	}

	// --- STEP 3: OVERWRITE payload prices with database values ---
	orderType := req.Order.OrderType

	for i := range req.Order.Products {
		product := &req.Order.Products[i]

		// Overwrite product price based on order type and database values
		officialPrices := officialProductPrices[product.ProductID]
		switch orderType {
		case "DELIVERY":
			product.Price = int(officialPrices["price_delivery"])
		case "TAKE_AWAY":
			product.Price = int(officialPrices["price_take_away"])
		default: // "IN"
			product.Price = int(officialPrices["price"])
		}

		log.Debug("Product price normalized from database",
			zap.String("product_id", product.ProductID),
			zap.String("order_type", orderType),
			zap.Int("official_price", product.Price),
		)

		// Overwrite configuration option prices with database values
		if product.Config != nil && product.Config.Attributes != nil {
			for _, attr := range product.Config.Attributes {
				for i, opt := range attr.Options {
					if officialPrice, exists := officialOptionPrices[opt.ID]; exists {
						attr.Options[i].ExtraPrice = officialPrice

						log.Debug("Option price normalized from database",
							zap.String("option_id", opt.ID),
							zap.Int("official_extra_price", officialPrice),
						)
					}
				}
			}
		}
	}

	return nil
}

func (s *Service) GetOrderSNO(ctx context.Context, qr, orderID string) (*models.Order, error) {
	log := logger.FromContext(ctx)

	// 🔹 1. Récupérer merchant via QR
	merchant, err := s.repo.GetMerchantByQR(ctx, qr) // déjà fait dans endpoint précédent
	if err != nil || merchant == nil {
		return nil, err
	}

	// 🔹 1. Appel OrderLifeCycle (comme PHP require_once)
	response, err := s.orderingService.ComputeGetOrder(ctx, merchant.MerchantID, orderID)
	if err != nil {
		log.Error("ComputeGetOrder", zap.Error(err))
		return nil, err
	}

	// 🔹 2. Chercher delivery session
	deliverySessionID, err := s.repo.GetDeliverySessionByOrderID(ctx, orderID)
	if err != nil {
		log.Error("GetDeliverySessionByOrderID", zap.String("order_id", orderID), zap.Error(err))
		return nil, err
	}

	// 🔹 3. Si session trouvée → enrichir
	if deliverySessionID != nil {
		log.Info("Order linked to delivery session",
			zap.String("order_id", orderID),
			zap.String("delivery_session_id", *deliverySessionID),
		)

		session, err := s.deliverySessionService.GetDeliverySession(ctx, merchant.MerchantID, *deliverySessionID)
		if err != nil {
			log.Error("GetDeliverySession", zap.String("delivery_session_id", *deliverySessionID), zap.Error(err))
			return nil, err
		}

		response.Orders[0].DeliverySession = session
	}

	return &response.Orders[0], nil
}

func (s *Service) CancelOrderSNO(ctx context.Context, qr, orderID string) (map[string]interface{}, error) {
	log := logger.FromContext(ctx)

	// 1️⃣ Merchant depuis QR
	merchantID, err := s.repo.GetMerchantIDByQR(ctx, qr)
	if err != nil {
		return nil, err
	}
	if merchantID == nil {
		return map[string]interface{}{"status": "cannot_retrieve_merchant"}, nil
	}

	// 2️⃣ Récupérer commande
	orderResp, err := s.GetOrderSNO(ctx, qr, orderID)
	if err != nil {
		return nil, err
	}
	if orderResp == nil {
		return map[string]interface{}{
			"status": "cannot_retrieve_order",
			"order":  orderID,
		}, nil
	}

	// 3️⃣ Vérifier état terminal
	state := fmt.Sprintf("%v", orderResp.State)
	if state == "CLOSED" || state == "DONE" {
		return map[string]interface{}{"status": "order_closed"}, nil
	}

	// 4️⃣ Garde merchant_approval : bloquer si le restaurant a déjà accepté
	if orderResp.MerchantApproval == "ACCEPTED" {
		return nil, models.ErrOrderAlreadyAccepted
	}

	// 5️⃣ Garde temporelle : bloquer après 60 secondes
	if time.Now().Unix()-time.Unix(orderResp.CreationDate, 0).Unix() > 60 {
		return nil, models.ErrTooLateToDeleteOrder
	}

	// 6️⃣ Récupérer le paiement Stripe s'il existe (non-bloquant si absent)
	var stripeIntentID, stripeAccountID string
	for _, p := range orderResp.Payments {
		if p.MOP == models.PaymentStripe && p.Enabled {
			stripeIntentID, stripeAccountID, err = s.repo.GetStripePaymentForOrder(ctx, orderID)
			if err != nil {
				// Non-bloquant : commande annulée même si lecture Stripe échoue.
				// Risque accepté : remboursement silencieusement absent — monitorer via logs.
				log.Error("CancelOrderSNO: stripe payment lookup failed, proceeding without refund",
					zap.String("order_id", orderID), zap.Error(err))
			}
			break
		}
	}

	// 7️⃣ Annulation DB complète (rewards, QR, payments, cache, intégrations)
	if err := s.orderLifeCycleSvc.DeleteOrder(ctx, models.DenyOrderInput{
		OrderID:            orderID,
		MerchantID:         *merchantID,
		UserID:             "SNO_CUSTOMER",
		DeletionReasonID:   "SNO_CUSTOMER_CANCELLED",
		DeletionReasonType: "scannorder",
		DeletionComment:    "Annulée par le client ScanNOrder",
	}); err != nil {
		return nil, err
	}

	// 8️⃣ Remboursement Stripe asynchrone (déclenché après succès de l'annulation DB)
	if stripeIntentID != "" && stripeAccountID != "" {
		s.StripeManager.RefundOrCancelAsync(stripeclient.RefundRequest{
			IntentID:  stripeIntentID,
			AccountID: stripeAccountID,
		})
	}

	return map[string]interface{}{"status": "cancelled"}, nil
}

func (s *Service) CreateOrderSNO(ctx context.Context, req *models.PricingRequest) (models.CreateOrderResult, error) {
	log := logger.FromContext(ctx)

	// 1️⃣ Merchant via QR
	merchant, err := s.repo.GetMerchantByQR(ctx, req.QRCode)
	if err != nil {
		log.Error("GetMerchantByQR", zap.Error(err))
		return models.CreateOrderResult{Status: "error_001"}, err
	}
	if merchant == nil {
		log.Error("GetMerchantByQR - no merchant found for qr " + req.QRCode)
		return models.CreateOrderResult{Status: "qr_code_expired"}, nil
	}

	req.Merchant = merchant
	req.MerchantID = merchant.MerchantID

	order := req.Order
	orderType := order.OrderType

	// 2️⃣ Vérif POS ouvert (sauf IN)
	if order.EstimatedReady == "" && orderType != "IN" {
		tz, _ := time.LoadLocation(merchant.Timezone)
		now := time.Now().In(tz)

		req.DayOfWeek = int(now.Weekday())
		if req.DayOfWeek == 0 {
			req.DayOfWeek = 7
		}
		// 1 = lundi, ..., 7 = dimanche (1-7 standard)
		req.Time = now.Format("2006-01-02 15:04:05")

		status, err := s.repo.GetMerchantStatus(ctx, req.MerchantID, req.DayOfWeek, req.Time)
		if err != nil {
			log.Error("GetMerchantStatus", zap.Error(err))
			return models.CreateOrderResult{Status: "error_002"}, err
		}
		if !status.IsOpen {
			return models.CreateOrderResult{Status: "pos_closed"}, nil
		}
	}

	// 3️⃣ SWITCH TYPE COMMANDE (LOGIQUE PHP IDENTIQUE)

	switch orderType {

	case "IN":
		customer, _ := s.repo.GetCustomerFromQR(ctx, req.QRCode)
		order.Customer = customer

		booking, _ := s.repo.GetBooking(ctx, req.QRCode)
		if booking != nil {
			order.BookingID = &booking.BookingID
		}

		order.MerchantApproval = "ACCEPTED"
		order.BrandStatus = "PENDING"

		/*
			if merchant.LocationID != "" {
				append(order.Locations, models.OrderLocation{
					LocationID: merchant.LocationID,
				})
			}

		*/

	case "DELIVERY":
		zone_result := s.CustomerInDeliveryZone(*merchant, *order.Customer)
		if !zone_result.InZone {
			return models.CreateOrderResult{Status: "address_too_far",
				Message: "Address located at " + strconv.Itoa(int(zone_result.DistanceMeters)) + "m from merchant (limited at " + strconv.Itoa(int(merchant.DeliveryDistanceLimit)) + "m for merchant " + merchant.MerchantID + ")"}, nil
		}

		order.OnlinePayment = true
		order.MerchantApproval = "PENDING_APPROVAL"

		// TODO nettoyage client
		// 🔥 Nettoyage EXACT PHP
		/*
			customer := order.Customer
			delete(customer, "customer_id")
			delete(customer, "customer_door_number")
			delete(customer, "customer_floor_number")
			delete(customer, "customer_additional_address")
			delete(customer, "customer_business_name")
			delete(customer, "customer_birthdate")
			delete(customer, "customer_additional_info")
			delete(customer, "customer_temporary_address")
			delete(customer, "customer_temporary_lat")
			delete(customer, "customer_temporary_lng")
			delete(customer, "customer_temporary_door_number")
			delete(customer, "customer_temporary_floor_number")
			delete(customer, "customer_temporary_additional_address")
		*/

		fallthrough

	case "TAKE_AWAY":
		if order.Customer.Tel != nil {
			order.Customer.MerchantID = &merchant.MerchantID
			customer, _ := s.repo.GetCustomerByPhone(ctx, *order.Customer)
			order.Customer = customer
		}

		order.OnlinePayment = true
		order.MerchantApproval = "PENDING_APPROVAL"
	}

	// 4️⃣ PRICING
	pricingResp, err := s.GetPricingSNO(ctx, req)
	if err != nil {
		log.Error("GetPricingSNO", zap.Error(err))
		return models.CreateOrderResult{Status: "error_003"}, err
	}

	if pricingResp.Status != "success" {
		return models.CreateOrderResult{Status: pricingResp.Status}, nil
	}

	orderReq := pricingResp.OrderRequest
	order = orderReq.Order

	// 5️⃣ Champs internes
	var ScannorderOwner = "SCANNORDER"
	order.CreatedBy = &ScannorderOwner
	order.IsSNO = true
	order.Payments = []models.PaymentPayload{}
	order.CashRegisterId = &ScannorderOwner

	// 6️⃣ Création commande BDD
	newOrder, err := s.orderLifeCycleSvc.CreateOrder(ctx, &models.RequestObject{
		MerchantID: orderReq.Merchant.MerchantID,
		Order:      *orderReq.Order,
	})
	if err != nil {
		log.Error("CreateOrder", zap.Error(err))
		return models.CreateOrderResult{Status: "error_003"}, err
	}

	if (newOrder.Status == "1" || newOrder.Status == "success") && newOrder.Action == "payment" {

		order.OrderID = &newOrder.OrderID
		req.CheckoutSessionType = "full_order"

		checkout, err := s.StripeManager.CreateCheckoutSession(stripeclient.CheckoutSessionRequestObject{
			Order:               order,
			Merchant:            merchant,
			QRCode:              req.QRCode,
			CheckoutSessionType: req.CheckoutSessionType,
			BaseURL:             s.cfg.SNORedirectBaseURL,
		})

		if err != nil {
			log.Error("CreateOrder", zap.Error(err))
			return models.CreateOrderResult{Status: "error_004"}, err
		}

		newOrder.CheckoutSession = &models.WRCheckoutSession{
			Status:      "success",
			RedirectURL: checkout.URL,
			URL:         checkout.URL,
		}
	}

	return *newOrder, nil
}

func (s *Service) CustomerInDeliveryZone(merchant models.MerchantRow, customer models.CustomerRequest) DeliveryZoneResult {
	if customer.Lat == nil || customer.Lng == nil {
		return DeliveryZoneResult{
			InZone: false,
		}
	}
	const earthRadius = 6371000.0 // mètres

	lat1 := merchant.Lat * math.Pi / 180
	lon1 := merchant.Lng * math.Pi / 180
	lat2 := *customer.Lat * math.Pi / 180
	lon2 := *customer.Lng * math.Pi / 180

	dLat := lat2 - lat1
	dLon := lon2 - lon1

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	distanceMeters := earthRadius * c
	distanceKm := distanceMeters / 1000

	return DeliveryZoneResult{
		InZone:         merchant.DeliveryDistanceLimit >= distanceMeters,
		DistanceMeters: distanceMeters,
		DistanceKm:     distanceKm,
		EstimatedFee:   int(merchant.DeliveryFees),
	}
}

// CheckDeliveryZone vérifie si un client est dans la zone de livraison
// et retourne les informations de livraison (frais minima, frais de livraison)
func (s *Service) CheckDeliveryZone(ctx context.Context, qrCode string, req *DeliveryCheckRequest) (*DeliveryCheckResponse, error) {
	log := logger.FromContext(ctx)

	log.Info("CheckDeliveryZone called", zap.String("qr_code", qrCode), zap.Float64("lat", req.Lat), zap.Float64("lng", req.Lng))

	// 1️⃣ Retrieve merchant from QR code
	merchant, err := s.repo.GetMerchantByQR(ctx, qrCode)
	if err != nil {
		log.Error("GetMerchantByQR failed", zap.Error(err))
		return &DeliveryCheckResponse{
			Status:  "error",
			Message: "Failed to retrieve merchant information",
		}, err
	}

	if merchant == nil {
		log.Warn("QR code expired or invalid", zap.String("qr_code", qrCode))
		return &DeliveryCheckResponse{
			Status:  "qr_code_expired",
			Message: "QR code is expired or invalid",
		}, nil
	}

	// 2️⃣ Convert the delivery check request coordinates to CustomerRequest format
	customer := models.CustomerRequest{
		Lat: &req.Lat,
		Lng: &req.Lng,
	}

	// 3️⃣ Check if customer is in delivery zone
	zoneResult := s.CustomerInDeliveryZone(*merchant, customer)

	log.Info("Delivery zone check result",
		zap.Bool("in_zone", zoneResult.InZone),
		zap.Float64("distance_km", zoneResult.DistanceKm),
		zap.Float64("distance_limit_km", merchant.DeliveryDistanceLimit/1000),
		zap.Int("estimated_fee", zoneResult.EstimatedFee),
	)

	// 4️⃣ Prepare response based on zone check
	if !zoneResult.InZone {
		return &DeliveryCheckResponse{
			Status:                  "out_of_delivery_zone",
			Message:                 "Désolé, nous ne livrons pas encore dans cette zone.",
			DistanceKm:              zoneResult.DistanceKm,
			DeliveryDistanceLimitKm: merchant.DeliveryDistanceLimit / 1000,
		}, nil
	}

	// 5️⃣ Extract minimum cart amount and delivery fees from merchant data
	// Using the DeliveryFeesLimit as minimum order amount
	// and DeliveryFees or EstimatedFee based on distance
	minOrderAmount := merchant.DeliveryFeesLimit
	deliveryFee := zoneResult.EstimatedFee

	return &DeliveryCheckResponse{
		Status:                  "in_delivery_zone",
		MinOrderAmount:          minOrderAmount,
		DeliveryFee:             deliveryFee,
		DistanceKm:              zoneResult.DistanceKm,
		DeliveryDistanceLimitKm: merchant.DeliveryDistanceLimit / 1000,
	}, nil
}

// GetLoyaltyPrograms retrieves active loyalty programs for the QR code's merchant
func (s *Service) GetLoyaltyPrograms(ctx context.Context, qrCode string, deliveryType string) (*LoyaltyProgramsResponse, error) {
	log := logger.FromContext(ctx)

	log.Info("GetLoyaltyPrograms called", zap.String("qr_code", qrCode), zap.String("delivery_type", deliveryType))

	// 1️⃣ Get merchant ID from QR code
	merchantID, _, err := s.repo.GetMerchantIDAndTZFromQR(ctx, qrCode)
	if err != nil || merchantID == "" {
		log.Warn("Merchant not found for QR code", zap.String("qr_code", qrCode), zap.Error(err))
		return &LoyaltyProgramsResponse{
			LoyaltyPrograms: []LoyaltyProgram{},
		}, nil
	}

	// 2️⃣ If no deliveryType provided, default to "DELIVERY"
	if deliveryType == "" {
		deliveryType = "DELIVERY"
	}

	log.Debug("Retrieving loyalty programs", zap.String("merchant_id", merchantID), zap.String("delivery_type", deliveryType))

	// 3️⃣ Retrieve loyalty programs from repository
	loyaltyPrograms, err := s.repo.GetLoyaltyPrograms(ctx, merchantID, deliveryType)
	if err != nil {
		log.Error("GetLoyaltyPrograms repo error", zap.Error(err))
		return nil, err
	}

	// 4️⃣ Return empty array if nil
	if loyaltyPrograms == nil {
		loyaltyPrograms = []LoyaltyProgram{}
	}

	log.Info("GetLoyaltyPrograms success", zap.Int("count", len(loyaltyPrograms)))

	return &LoyaltyProgramsResponse{
		LoyaltyPrograms: loyaltyPrograms,
	}, nil
}

// GetDiscounts retrieves active discounts for the QR code's merchant
func (s *Service) GetDiscounts(ctx context.Context, qrCode string, deliveryType string) (*DiscountsResponse, error) {
	log := logger.FromContext(ctx)

	log.Info("GetDiscounts called", zap.String("qr_code", qrCode), zap.String("delivery_type", deliveryType))

	// 1️⃣ Get merchant ID and timezone from QR code
	merchantID, tz, err := s.repo.GetMerchantIDAndTZFromQR(ctx, qrCode)
	if err != nil || merchantID == "" {
		log.Warn("Merchant not found for QR code", zap.String("qr_code", qrCode), zap.Error(err))
		return &DiscountsResponse{
			Discounts: []Discount{},
		}, nil
	}

	// 2️⃣ If no deliveryType provided, default to "DELIVERY"
	if deliveryType == "" {
		deliveryType = "DELIVERY"
	}

	// 3️⃣ Get current day of week in merchant's timezone
	loc, _ := time.LoadLocation(tz)
	now := time.Now().In(loc)
	dow := int(now.Weekday())
	if dow == 0 {
		dow = 7
	}

	log.Debug("Retrieving discounts", zap.String("merchant_id", merchantID), zap.String("delivery_type", deliveryType), zap.Int("day_of_week", dow))

	// 4️⃣ Retrieve discounts from repository
	discounts, err := s.repo.GetDiscounts(ctx, merchantID, deliveryType, dow)
	if err != nil {
		log.Error("GetDiscounts repo error", zap.Error(err))
		return nil, err
	}

	// 5️⃣ Return empty array if nil
	if discounts == nil {
		discounts = []Discount{}
	}

	log.Info("GetDiscounts success", zap.Int("count", len(discounts)))

	return &DiscountsResponse{
		Discounts: discounts,
	}, nil
}

// GetUpsell retrieves famous (upsell) products for the QR code's merchant, fully configured
// (attributes, options) so the frontend can open the product configuration modal directly.
func (s *Service) GetUpsell(ctx context.Context, qr string) (*UpsellResponse, error) {
	// Si Redis n'est pas configuré, on court direct à la BDD
	if s.redis == nil {
		return s.computeGetUpsell(ctx, qr)
	}

	log := logger.FromContext(ctx)
	cacheKey := models.ScannorderMerchantUpsell + qr

	// --- ÉTAPE 1 : Chercher dans Redis ---
	cached, found := s.redis.Get(ctx, cacheKey)

	if found {
		var upsell UpsellResponse
		if err := json.Unmarshal([]byte(cached), &upsell); err == nil {
			log.Info("🧠🎯 Upsell found in Redis cache 🎯🧠")
			return &upsell, nil
		}
	}

	log.Info("🧠🚫 Upsell not found in Redis cache 🚫🧠")

	// --- ÉTAPE 2 : Appel BDD (calcul lourd : 1 GetProduct par produit populaire) ---
	upsell, err := s.computeGetUpsell(ctx, qr)
	if err != nil {
		return nil, err
	}

	if upsell == nil {
		return nil, nil
	}

	// --- ÉTAPE 3 : Stocker dans Redis ---
	// Note : pas d'invalidation active ici, comme pour le cache menu (DeleteAllMerchantKeys
	// existe dans internal/infrastructure/redis/client.go mais n'est appelé nulle part
	// actuellement) — on se repose uniquement sur le TTL, mêmes règles que GetMenu.
	serialized, err := json.Marshal(upsell)
	if err == nil {
		if saved := s.redis.Set(ctx, cacheKey, string(serialized), models.ScannorderMerchantMenuTTL); !saved {
			log.Warn("Warning Redis Set (Upsell): save failed")
		} else {
			log.Info("🧠📌 Upsell saved in Redis cache 📌🧠")
		}
	}

	return upsell, nil
}

func (s *Service) computeGetUpsell(ctx context.Context, qrCode string) (*UpsellResponse, error) {
	log := logger.FromContext(ctx)

	log.Info("GetUpsell called", zap.String("qr_code", qrCode))

	// 1️⃣ Get merchant ID from QR code
	merchantID, _, err := s.repo.GetMerchantIDAndTZFromQR(ctx, qrCode)
	if err != nil || merchantID == "" {
		log.Warn("Merchant not found for QR code", zap.String("qr_code", qrCode), zap.Error(err))
		return &UpsellResponse{
			Products: []models.ProductEntry{},
		}, nil
	}

	log.Debug("Retrieving upsell product IDs", zap.String("merchant_id", merchantID))

	// 2️⃣ Retrieve popular (is_popular = 1) product IDs from repository
	productIDs, err := s.repo.GetUpsellProducts(ctx, merchantID)
	if err != nil {
		log.Error("GetUpsellProducts repo error", zap.Error(err))
		return nil, err
	}

	// 3️⃣ Load each product fully configured, same as the product detail endpoint
	products := make([]models.ProductEntry, 0, len(productIDs))
	for _, productID := range productIDs {
		product, err := s.menu.GetProductFromMerchantId(ctx, merchantID, productID)
		if err != nil {
			// Un produit en erreur ne doit pas bloquer toute la réponse upsell
			log.Warn("GetUpsell: failed to load product, skipping", zap.String("product_id", productID), zap.Error(err))
			continue
		}
		if product == nil {
			continue
		}

		s.cleanProductForSNO(product, "")
		products = append(products, *product)
	}

	log.Info("GetUpsell success", zap.Int("count", len(products)))

	return &UpsellResponse{
		Products: products,
	}, nil
}

func (s *Service) cleanProductPricesForSNO(product *models.ProductEntry, deliveryType string) {
	switch deliveryType {
	case "DELIVERY":
		product.Price = *product.PriceDelivery
	case "TAKE_AWAY":
		product.Price = *product.PriceTakeAway
	}

	product.PriceDelivery = nil
	product.PriceTakeAway = nil
	product.PriceDeliveroo = nil
	product.PriceUberEats = nil
}

// cleanProductForSNO nettoie et adapte un produit pour la réponse SNO
// - Modifie le prix selon le type de commande (DELIVERY/TAKE_AWAY)
// - Supprime les attributs non pertinents (prix alternatifs, couleurs, TVAs)
func (s *Service) cleanProductForSNO(product *models.ProductEntry, deliveryType string) {
	// 1️⃣ Adapter le prix selon le type de commande
	s.cleanProductPricesForSNO(product, deliveryType)

	// 2️⃣ Nettoyer les attributs non pertinents
	product.BgColor = nil
	product.Category = nil
	product.TVAIn = nil
	product.TVADelivery = nil
	product.TVATakeAway = nil
	product.IsAvailableOnSNO = nil
	product.IsProductGroup = nil
	product.SubProducts = nil
	product.SyncUberEats = nil
	product.SyncDeliveroo = nil
	product.MerchantID = nil
	product.IsAvailableOnSNO = nil
	product.Available = nil
	product.AvailableIn = nil
	product.AvailableDelivery = nil
	product.AvailableTakeAway = nil
	product.MarginPercent = nil
	product.FoodCostPercent = nil
	product.IsDistributed = nil
	product.ProductionColor = nil

}

func (s *Service) GetProduct(ctx context.Context, qr string, productID string, deliveryType string) (*models.ProductEntry, error) {
	log := logger.FromContext(ctx)

	// 1️⃣ Récupérer le merchantID depuis le QR code
	merchantID, _, err := s.repo.GetMerchantIDAndTZFromQR(ctx, qr)
	if err != nil || merchantID == "" {
		log.Error("GetProduct: Merchant not found from QR", zap.String("qr_code", qr), zap.Error(err))
		return nil, fmt.Errorf("merchant_not_found")
	}

	// 2️⃣ Récupérer le produit via le service menu
	product, err := s.menu.GetProductFromMerchantId(ctx, merchantID, productID)
	if err != nil {
		log.Error("GetProduct: Failed to fetch product", zap.String("merchant_id", merchantID), zap.String("product_id", productID), zap.Error(err))
		return nil, err
	}

	if product == nil {
		log.Warn("GetProduct: Product not found", zap.String("merchant_id", merchantID), zap.String("product_id", productID))
		return nil, fmt.Errorf("product_not_found")
	}

	// 3️⃣ Vérifier is_available_on_sno
	if product.IsAvailableOnSNO == nil || !*product.IsAvailableOnSNO {
		log.Warn("GetProduct: Product not available on SNO", zap.String("merchant_id", merchantID), zap.String("product_id", productID))
		return nil, fmt.Errorf("product_not_available_on_sno")
	}

	// 4️⃣ Nettoyer et adapter le produit pour SNO
	s.cleanProductForSNO(product, deliveryType)

	log.Info("GetProduct success", zap.String("merchant_id", merchantID), zap.String("product_id", productID), zap.String("delivery_type", deliveryType))

	return product, nil
}

func (s *Service) GetSlots(ctx context.Context, qr string) (*SlotsResponse, error) {
	log := logger.FromContext(ctx)

	// 1️⃣ Récupérer le merchant via QR code
	merchant, err := s.repo.GetMerchantByQR(ctx, qr)
	if err != nil {
		log.Error("GetSlots: Merchant retrieval failed", zap.String("qr_code", qr), zap.Error(err))
		return &SlotsResponse{
			Status: "qr_code_expired",
		}, nil
	}

	if merchant == nil {
		log.Warn("GetSlots: Merchant not found", zap.String("qr_code", qr))
		return &SlotsResponse{
			Status: "qr_code_expired",
		}, nil
	}

	// 2️⃣ Vérifier que les commandes avancées sont activées
	// Note: On suppose que EnableAdvanceOrders est une propriété du merchant récupéré
	// Si elle n'existe pas, vous pouvez hardcoder à true ou l'ajouter à la récupération du merchant
	if !merchant.EnableAdvanceOrders {
		log.Warn("GetSlots: Advance orders not enabled for merchant", zap.String("merchant_id", merchant.MerchantID))
		return &SlotsResponse{
			Status: "advance_orders_disabled",
			Error:  "Advanced orders are not enabled for this merchant",
		}, nil
	}

	// 3️⃣ Récupérer le temps de préparation effectif
	merchantRow := &models.MerchantRow{
		MerchantID:   merchant.MerchantID,
		Timezone:     merchant.Timezone,
		PrepTimeMode: merchant.PrepTimeMode,
		PrepTime:     merchant.PrepTime,
	}
	prepMinutes := s.GetEffectivePrepMinutes(ctx, merchantRow)

	loc, err := time.LoadLocation(merchant.Timezone)
	if err != nil {
		log.Warn("GetSlots: Invalid merchant timezone, fallback to UTC", zap.String("merchant_id", merchant.MerchantID), zap.String("timezone", merchant.Timezone), zap.Error(err))
		loc = time.UTC
	}

	now := time.Now().In(loc)
	localDate := now.Format("2006-01-02")
	localTime := now.Format("15:04:05")

	// 4️⃣ Récupérer les slots disponibles
	availableSlots, err := s.repo.GetAvailableSlots(ctx, merchant.MerchantID, prepMinutes, localDate, localTime)
	if err != nil {
		log.Error("GetSlots: Failed to retrieve slots", zap.String("merchant_id", merchant.MerchantID), zap.Error(err))
		return nil, err
	}

	// 5️⃣ Retourner les slots
	if availableSlots == nil {
		availableSlots = make(map[string][]TimeSlot)
	}

	availableSlotsList := make([]SlotsByDate, 0, len(availableSlots))
	dates := make([]string, 0, len(availableSlots))
	for date := range availableSlots {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	for _, date := range dates {
		availableSlotsList = append(availableSlotsList, SlotsByDate{
			Date:  date,
			Slots: availableSlots[date],
		})
	}

	log.Info("GetSlots success", zap.String("merchant_id", merchant.MerchantID), zap.Int("slot_count", len(availableSlots)))

	return &SlotsResponse{
		Status:         "1",
		AvailableSlots: availableSlotsList,
	}, nil
}
