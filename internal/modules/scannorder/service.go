package scannorder

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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

	openStatus, err := s.IsMerchantOpen(ctx, row.MerchantID)
	if err != nil {
		return nil, err
	}
	// 1. Calculer le temps de préparation effectif
	prepMinutes := s.GetEffectivePrepMinutes(ctx, row)

	// 2. Récupérer les slots en passant ce délai
	availableSlots, err := s.repo.GetAvailableSlots(ctx, row.MerchantID, prepMinutes)
	if err != nil {
		// Optionnel : on log l'erreur mais on continue avec un map vide
		// pour ne pas bloquer tout l'affichage du marchand
		availableSlots = make(map[string][]TimeSlot)
	}

	resp := &MerchantResponse{
		Status: "success",
		Merchant: &MerchantData{
			MerchantID:      row.MerchantID,
			BusinessName:    row.FullName,
			Currency:        row.Currency,
			IsOpen:          openStatus.OpenHours && openStatus.OpenStatus,
			Status:          openStatus,
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

			AdvanceOrder: AdvanceOrder{
				EnableAdvanceOrders: true, // Ou row.EnableAdvanceOrders si tu l'as en base
				AvailableSlots:      availableSlots,
			},
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
	}

	resp.Merchant.Fee = MerchantFees{
		DeliveryFees:      row.DeliveryFees,
		DeliveryFeesLimit: row.DeliveryFeesLimit,
	}

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

	rawMenu, err := s.menu.GetMenuFromMerchantId(ctx, merchantID, nil) // 🔥 PLACEHOLDER
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

		for _, p := range products {
			product := p

			switch deliveryType {
			case "DELIVERY":
				product.Price = *product.PriceDelivery
			case "TAKE_AWAY":
				product.Price = *product.PriceTakeAway
			}

			product.BgColor = nil
			product.Category = nil
			product.TVAIn = nil
			product.TVADelivery = nil
			product.TVATakeAway = nil
			product.PriceDelivery = nil
			product.PriceTakeAway = nil
			/*
				delete(product, "bg_color")
				delete(product, "category")
				delete(product, "tva_rate_delivery")
				delete(product, "tva_rate_in")
				delete(product, "tva_rate_take_away")
				delete(product, "price_delivery")
				delete(product, "price_take_away")

			*/

			if product.IsProductGroup || !product.IsAvailableOnSNO {
				if len(product.SubProducts) > 0 {
					toAdd = append(toAdd, product.SubProducts...)
				}
				continue
			}
			finalProducts = append(finalProducts, product)
		}

		for _, sp := range toAdd {
			sub := sp
			if sub.IsAvailableOnSNO {
				finalProducts = append(finalProducts, sub)
			}
		}

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

func (s *Service) IsMerchantOpen(ctx context.Context, merchantID string) (*MerchantOpenStatus, error) {

	// On récupère timezone pour reproduire PHP
	_, tz, err := s.repo.GetMerchantIDAndTZFromMerchantID(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	loc, _ := time.LoadLocation(tz)
	now := time.Now().In(loc)

	dow := int(now.Weekday())
	if dow == 0 {
		dow = 7
	}

	currentTime := now.Format("15:04:05")

	return s.repo.GetMerchantOpenStatus(ctx, merchantID, dow, currentTime)
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
		openStatus, err := s.IsMerchantOpen(ctx, row.MerchantID)
		isOpen := false
		if err == nil {
			isOpen = openStatus.OpenHours && openStatus.OpenStatus
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
			TakeawayEnabled: row.TakeawayEnabled,
			DeliveryEnabled: row.DeliveryEnabled,
		})
	}

	return &BrandResponse{
		Status:    "success",
		Brand:     brand,
		Merchants: summaries,
	}, nil
}

func (s *Service) GetPricingSNO(ctx context.Context, req *models.PricingRequest) (*models.PricingResponse, error) {

	log := logger.FromContext(ctx)

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

	// 🔹 4. Timezone logic (IDENTIQUE PHP)
	loc, _ := time.LoadLocation(merchant.Timezone)
	now := time.Now().In(loc)

	req.MerchantID = merchant.MerchantID
	req.IsSNO = true
	req.DayOfWeek = int(now.Weekday())
	if req.DayOfWeek == 0 {
		req.DayOfWeek = 7
	}
	req.Time = now.Format("2006-01-02 15:04:05")

	log.Info("SNO pricing context prepared",
		zap.String("merchant_id", req.MerchantID),
		zap.Int("dow", req.DayOfWeek),
	)

	// 🔹 6. Appel module ORDERING (comme PHP require_once)
	return s.orderingService.ComputePricing(ctx, req)
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
		return nil, err
	}

	// 🔹 2. Chercher delivery session
	deliverySessionID, err := s.repo.GetDeliverySessionByOrderID(ctx, orderID)
	if err != nil {
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

	// 2️⃣ Récupérer commande (via module déjà fait)
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

	// 3️⃣ Vérifier état
	state := fmt.Sprintf("%v", orderResp.State)
	if state == "CLOSED" || state == "DONE" {
		return map[string]interface{}{
			"status": "order_closed",
		}, nil
	}

	// 4️⃣ Vérifier délai 150 sec
	creationStr := fmt.Sprintf("%v", orderResp.CreationDate)
	creationTime, err := time.Parse("2006-01-02 15:04:05", creationStr)
	if err != nil {
		log.Error("Failed to parse creation date", zap.String("creation_date", creationStr), zap.Error(err))
		return nil, err
	}

	now := time.Now().Unix()
	calc := now - creationTime.Unix()

	if calc > 150 {
		return map[string]interface{}{"status": "too_late_to_delete_order"}, nil
	}

	// 🐛 RETURN DEBUG EXACT COMME PHP
	return map[string]interface{}{
		"calc":          calc,
		"now":           now,
		"creation_date": creationTime.Unix(),
	}, nil

	// --- CODE INATTEIGNABLE MAIS PRÉSENT EN PHP ---

	/*
		if int64(orderRaw["merchant_id"].(float64)) != *merchantID {
			return map[string]interface{}{
				"status":   "cannot_delete_this_order_from_this_merchant",
				"order":    orderRaw,
				"merchant": merchantID,
			}, nil
		}

		return s.orderLifeCycleSvc.SetDeleted(ctx, *merchantID, orderID, "SCANNORDER", "3", "")
	*/
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
		req.Time = now.Format("2006-01-02 15:04:05")

		openStatus, err := s.repo.GetMerchantOpenStatus(ctx, req.MerchantID, req.DayOfWeek, req.Time)
		if err != nil {
			log.Error("GetMerchantOpenStatus", zap.Error(err))
			return models.CreateOrderResult{Status: "error_002"}, err
		}
		if !openStatus.OpenHours || !openStatus.OpenStatus {
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
			Status: "success",
			URL:    checkout.URL,
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

	// Même logique PHP
	var price int
	if distanceKm < 5 {
		price = 5
	} else if distanceKm < 10 {
		price = 10
	} else {
		price = 15
	}

	return DeliveryZoneResult{
		InZone:         merchant.DeliveryDistanceLimit >= distanceMeters,
		DistanceMeters: distanceMeters,
		DistanceKm:     distanceKm,
		EstimatedFee:   price,
	}
}
