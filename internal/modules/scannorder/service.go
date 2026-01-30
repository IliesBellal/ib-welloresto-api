package scannorder

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/menu"

	"go.uber.org/zap"
)

type Service struct {
	repo *Repository
	menu *menu.MenuService
}

func NewService(r *Repository, m *menu.MenuService) *Service {
	return &Service{repo: r, menu: m}
}

func (s *Service) GetMerchant(ctx context.Context, qr string) (*MerchantResponse, error) {
	row, err := s.repo.GetMerchantByQR(ctx, qr)
	if err != nil {
		return &MerchantResponse{Status: "0"}, nil
	}

	// 🔥 Expiration QR (2h)
	if row.CreationDate.Valid {
		if time.Since(row.CreationDate.Time) > 2*time.Hour {
			return &MerchantResponse{
				Status: "qr_code_expired",
				Error:  "Qr Code expired",
			}, nil
		}
	}

	openStatus, err := s.IsMerchantOpen(ctx, data.MerchantID)
	if err != nil {
		return nil, err
	}

	resp := &MerchantResponse{
		Status: "1",
		Merchant: &MerchantData{
			MerchantID:   row.MerchantID,
			BusinessName: row.FullName,
			Phone:        "",
			Currency:     row.Currency,
			IsOpen:       openStatus.OpenHours && openStatus.OpenStatus,
			Status:       openStatus,
			AdvanceOrder: struct {
				EnableAdvanceOrders bool                `json:"enable_advance_orders"`
				AvailableSlots      map[string][]string `json:"available_slots"`
			}{
				EnableAdvanceOrders: true,
				AvailableSlots:      map[string][]string{},
			},
		},
	}

	resp.Merchant.Address.Address = row.Address
	resp.Merchant.Address.Lat = row.Lat
	resp.Merchant.Address.Lng = row.Lng

	resp.Merchant.Design.PrimaryColor = row.PrimaryColor
	resp.Merchant.Design.TextColor = row.TextColor

	resp.Merchant.Fee.DeliveryFees = row.DeliveryFees
	resp.Merchant.Fee.DeliveryFeesLimit = row.DeliveryFeesLimit

	resp.Merchant.QRCode.MenuOnly = row.MenuOnly
	resp.Merchant.QRCode.UserID = nullableInt64(row.UserID)
	resp.Merchant.QRCode.LastWaiterCall = nullableString(row.LastWaiterCall)
	resp.Merchant.QRCode.OrderID = nullableInt64(row.OrderID)
	resp.Merchant.QRCode.LocationID = nullableInt64(row.LocationID)
	resp.Merchant.QRCode.LocationName = nullableString(row.LocationName)

	return resp, nil
}

func nullableInt64(n sql.NullInt64) *int64 {
	if n.Valid {
		return &n.Int64
	}
	return nil
}

func nullableString(s sql.NullString) *string {
	if s.Valid {
		return &s.String
	}
	return nil
}

func (s *Service) GetMenu(ctx context.Context, qr string, deliveryType string) (*MenuResponse, error) {

	merchantID, tz, err := s.repo.GetMerchantIDAndTZ(ctx, qr)
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
		var toAdd []interface{}

		for _, p := range products {
			product := p

			switch deliveryType {
			case "DELIVERY":
				product.Price = product.PriceDelivery
			case "TAKE_AWAY":
				product.Price = product.PriceTakeAway
			}
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
				if subs, ok := product["sub_products"].([]interface{}); ok {
					toAdd = append(toAdd, subs...)
				}
				continue
			}
			finalProducts = append(finalProducts, product)
		}

		for _, sp := range toAdd {
			sub := sp.(map[string]interface{})
			if sub["is_available_on_sno"] == float64(1) {
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
	_, tz, err := s.repo.GetMerchantIDAndTZ(ctx, merchantID)
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

func (s *Service) GetPricingSNO(ctx context.Context, req *PricingSNORequest) (interface{}, error) {

	log := logger.FromContext(ctx)

	// 🔹 1. Récupérer merchant via QR
	merchant, err := s.repo.GetMerchantByQR(ctx, req.QRCode) // déjà fait dans endpoint précédent
	if err != nil || merchant == nil {
		return map[string]interface{}{
			"status":   "-1",
			"error":    "QR Code expired",
			"merchant": merchant,
		}, nil
	}

	// 🔹 2. Delivery zone check
	if req.Order.OrderType == "DELIVERY" &&
		req.Order.Customer != nil {

		inZone := s.customerInDeliveryZone(merchant, req.Order.Customer) // placeholder
		req.IsInDeliveryZone = inZone
	}

	// 🔹 3. Enrich customer
	if req.Order.Customer != nil {
		req.Order.Customer.MerchantID = merchant.ID
		customer, _ := s.repo.GetCustomerByPhone(ctx, req.Order.Customer) // placeholder
		req.Order.Customer = customer
	}

	// 🔹 4. Timezone logic (IDENTIQUE PHP)
	loc, _ := time.LoadLocation(merchant.Timezone)
	now := time.Now().In(loc)

	req.MerchantID = merchant.ID
	req.IsSNO = true
	req.DayOfWeek = int(now.Weekday())
	if req.DayOfWeek == 0 {
		req.DayOfWeek = 7
	}
	req.Time = now.Format("2006-01-02 15:04:05")

	log.Info("SNO pricing context prepared",
		zap.Int64("merchant_id", req.MerchantID),
		zap.Int("dow", req.DayOfWeek),
	)

	// 🔹 5. Vérifier produits indisponibles
	unavailableMap, err := s.repo.GetUnavailableProducts(ctx, req.MerchantID, req.DayOfWeek, now.Format("15:04:05"))
	if err != nil {
		return nil, err
	}

	var notAvailable []map[string]interface{}
	for _, p := range req.Order.Products {
		if name, ok := unavailableMap[p.ProductID]; ok {
			notAvailable = append(notAvailable, map[string]interface{}{
				"product_id": p.ProductID,
				"name":       name,
			})
		}
	}

	if len(notAvailable) > 0 {
		return map[string]interface{}{
			"status":                 "not_available_products",
			"not_available_products": notAvailable,
		}, nil
	}

	// 🔹 6. Appel module ORDERING (comme PHP require_once)
	return s.orderingService.GetPricing(ctx, req)
}

func (s *Service) GetOrderSNO(ctx context.Context, orderID int64) (map[string]interface{}, error) {
	log := logger.FromContext(ctx)

	// 🔹 1. Appel OrderLifeCycle (comme PHP require_once)
	response, err := s.orderLifeCycleSvc.GetOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}

	// 🔹 2. Chercher delivery session
	merchantID, deliverySessionID, err := s.repo.GetDeliverySessionByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}

	// 🔹 3. Si session trouvée → enrichir
	if deliverySessionID != nil && merchantID != nil {
		log.Info("Order linked to delivery session",
			zap.Int64("order_id", orderID),
			zap.Int64("delivery_session_id", *deliverySessionID),
		)

		session, err := s.managementSvc.GetDeliverySession(ctx, *merchantID, *deliverySessionID)
		if err != nil {
			return nil, err
		}

		response["delivery_session"] = session
	}

	return response, nil
}

func (s *Service) CancelOrderSNO(ctx context.Context, qr string, orderID int64) (map[string]interface{}, error) {
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
	orderResp, err := s.GetOrderSNO(ctx, orderID)
	if err != nil {
		return nil, err
	}

	orderRaw, ok := orderResp["order"].(map[string]interface{})
	if !ok || orderRaw == nil {
		return map[string]interface{}{
			"status": "cannot_retrieve_order",
			"order":  orderRaw,
		}, nil
	}

	// 3️⃣ Vérifier état
	state := fmt.Sprintf("%v", orderRaw["state"])
	if state == "CLOSED" || state == "DONE" {
		return map[string]interface{}{"status": "order_closed"}, nil
	}

	// 4️⃣ Vérifier délai 150 sec
	creationStr := fmt.Sprintf("%v", orderRaw["creation_date"])
	creationTime, err := time.Parse("2006-01-02 15:04:05", creationStr)
	if err != nil {
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

func (s *Service) CreateOrderSNO(ctx context.Context, req *CreateOrderRequest) (map[string]interface{}, error) {

	// 1️⃣ Merchant via QR
	merchant, err := s.repo.GetMerchantByQR(ctx, req.QRCode)
	if err != nil {
		return map[string]interface{}{"status": "-2", "error": err.Error()}, nil
	}
	if merchant == nil {
		return map[string]interface{}{"status": "-1", "error": "QR Code expired"}, nil
	}

	req.Merchant = merchant
	req.MerchantID = int64(merchant["id"].(int64))

	order := req.Order
	orderType := order["order_type"].(string)

	// 2️⃣ Vérif POS ouvert (sauf IN)
	if order["estimated_ready"] == nil && orderType != "IN" {
		tz, _ := time.LoadLocation(merchant["timezone"].(string))
		now := time.Now().In(tz)

		req.DayOfWeek = int(now.Weekday())
		if req.DayOfWeek == 0 {
			req.DayOfWeek = 7
		}
		req.Time = now.Format("2006-01-02 15:04:05")

		openStatus, err := s.repo.GetMerchantOpenStatus(ctx, req.MerchantID, req.DayOfWeek, req.Time)
		if err != nil {
			return nil, err
		}
		if !openStatus.OpenHours || !openStatus.OpenStatus {
			return map[string]interface{}{"status": "pos_closed", "object": req}, nil
		}
	}

	// 3️⃣ SWITCH TYPE COMMANDE (LOGIQUE PHP IDENTIQUE)

	switch orderType {

	case "IN":
		customer, _ := s.OrderingService.GetCustomer(ctx, order)
		order["customer"] = customer

		booking, _ := s.OrderingService.GetBooking(ctx, order)
		if booking != nil {
			order["booking_id"] = booking["booking_id"]
		}

		order["merchant_approval"] = "ACCEPTED"
		order["brand_status"] = "PENDING"

		if merchant["location"] != nil {
			order["locations"] = []map[string]interface{}{
				{"location_id": merchant["location"]},
			}
		}

	case "DELIVERY":
		ok, _ := s.OrderingService.CustomerInDeliveryZone(ctx, merchant, order["customer"])
		if !ok {
			return map[string]interface{}{"status": "address_too_far"}, nil
		}

		order["online_payment"] = true
		customer := order["customer"].(map[string]interface{})

		// 🔥 Nettoyage EXACT PHP
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

		fallthrough

	case "TAKE_AWAY":
		if c, ok := order["customer"].(map[string]interface{}); ok {
			if c["customer_tel"] != nil {
				c["merchant_id"] = merchant["id"]
				customer, _ := s.OrderingService.GetCustomerByPhone(ctx, c)
				order["customer"] = customer
			}
		}
	}

	// 4️⃣ PRICING
	pricingResp, err := s.GetPricingSNO(ctx, req)
	if err != nil {
		return nil, err
	}

	if pricingResp["status"] != "1" {
		return pricingResp, nil
	}

	orderReq := pricingResp["order_request"].(map[string]interface{})
	order = orderReq["order"].(map[string]interface{})

	// 5️⃣ Champs internes
	order["created_by"] = "SCANNORDER"
	order["is_sno"] = true
	order["payments"] = []interface{}{}

	// 6️⃣ Création commande BDD
	newOrder, err := s.OrderingService.AddNewOrder(ctx, orderReq)
	if err != nil {
		return nil, err
	}

	if newOrder["status"] == "1" && newOrder["action"] == "payment" {

		order["order_id"] = newOrder["order_id"]
		req.CheckoutSessionType = "full_order"

		checkout, err := s.StripeClient.CreateCheckoutSession(map[string]interface{}{
			"order":                 order,
			"merchant":              merchant,
			"qr_code":               req.QRCode,
			"checkout_session_type": req.CheckoutSessionType,
			"redirect_base_url":     s.cfg.SNORedirectBaseURL,
		})
		if err != nil {
			return map[string]interface{}{"status": "-4", "error": err.Error()}, nil
		}

		newOrder["checkout_session"] = map[string]interface{}{
			"status": "1",
			"url":    checkout.URL,
		}
	}

	return newOrder, nil
}

func (s *Service) CustomerInDeliveryZone(merchant Merchant, customer CustomerLocation) DeliveryZoneResult {
	const earthRadius = 6371000.0 // mètres

	lat1 := merchant.Lat * math.Pi / 180
	lon1 := merchant.Lng * math.Pi / 180
	lat2 := customer.CustomerLat * math.Pi / 180
	lon2 := customer.CustomerLng * math.Pi / 180

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
