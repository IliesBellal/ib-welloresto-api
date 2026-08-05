package orders

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/audit"
	"welloresto-api/internal/modules/notification"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

type OrdersService struct {
	ordersRepo           *OrdersRepository
	notificationsService *notification.NotificationService
	redis                *redis.Client
	sfGroup              singleflight.Group
	db                   *sql.DB
	auditService         audit.AuditService
}

func (s *OrdersService) ExistsByBrandOrderID(ctx context.Context, brand, brandOrderID string) (bool, error) {
	// Logique métier : on valide les entrées si nécessaire
	if brand == "" || brandOrderID == "" {
		return false, nil
	}

	// Appel au repository
	exists, err := s.ordersRepo.ExistsByBrandOrderID(ctx, brand, brandOrderID)
	if err != nil {
		// On log l'erreur mais on peut décider de retourner false
		// pour ne pas bloquer le flux en cas de pépin DB
		return false, fmt.Errorf("error checking order existence: %w", err)
	}

	return exists, nil
}

func NewOrdersService(ordersRepo *OrdersRepository, notificationsService *notification.NotificationService, redis *redis.Client, auditService audit.AuditService) *OrdersService {
	return &OrdersService{
		ordersRepo:           ordersRepo,
		notificationsService: notificationsService,
		redis:                redis,
		auditService:         auditService,
		db:                   ordersRepo.database, // On récupère la DB du repository pour les transactions

	}
}

func (s *OrdersService) logPricingStageError(ctx context.Context, stage string, err error, fields ...zap.Field) {
	if err == nil {
		return
	}

	logFields := []zap.Field{
		zap.String("pricing_stage", stage),
		zap.Error(err),
	}
	logFields = append(logFields, logger.DBFailureFields(s.db, err)...)
	logFields = append(logFields, fields...)

	logger.FromContext(ctx).Error("pricing dependency failed", logFields...)
}

/*
func (s *OrdersService) ReopenClosedOrder(ctx context.Context, orderID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}

	// user.MerchantID et user.UserID sont récupérés depuis le contexte

	return s.ordersRepo.ReopenClosedOrder(ctx, user.MerchantID, orderID, user.UserID)
}
*/
/*
Will use order life cycle module
func (s *OrdersService) AddPayment(ctx context.Context, orderID string, req *models.PaymentRequest) error {
	user, _ := middleware.UserFromContext(ctx)
	req.OrderID = orderID

	return dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		// Pour un paiement, on peut auditer soit l'objet "Order" qui change de statut isPaid,
		// soit simplement le nouvel objet "Payment". Auditer l'Order est plus sûr.
		oldOrder, _ := s.ComputeGetOrder(txCtx, user.MerchantID, orderID)

		if err := s.ordersRepo.AddPayment(txCtx, user.MerchantID, user.UserID, req); err != nil {
			return err
		}

		newOrder, _ := s.ComputeGetOrder(txCtx, user.MerchantID, orderID)

		return s.auditService.LogChange(txCtx, models.ActionPaymentAdded, models.ResourcePayment, orderID, oldOrder, newOrder)
	})
}
*/
// GetPendingOrderIDs : Service pour récupérer uniquement les identifiants des commandes en cours
func (s *OrdersService) GetPendingOrderIDs(ctx context.Context, app string) ([]string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.ordersRepo.GetPendingOrderIDs(ctx, user.MerchantID, app)
}

// GetOrdersByIDs : Service pour récupérer les objets commandes complets à partir d'une liste d'IDs
func (s *OrdersService) GetOrdersByIDs(ctx context.Context, orderIDs []string) ([]models.Order, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if len(orderIDs) == 0 {
		return []models.Order{}, nil
	}

	return s.ordersRepo.GetOrdersByIDs(ctx, user.MerchantID, orderIDs)
}

func (s *OrdersService) GetPendingOrders(ctx context.Context, app string) (*models.PendingOrdersResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	log := logger.FromContext(ctx)

	// 1. Récupération des IDs
	ids, err := s.GetPendingOrderIDs(ctx, app)
	if err != nil || len(ids) == 0 {
		return &models.PendingOrdersResponse{Orders: []models.Order{}}, err
	}

	var finalOrders = make([]models.Order, 0, len(ids))
	var missingIDs []string
	var cacheResults = make(map[string]models.Order)

	// 2. Tentative depuis Redis (On attend bien un modèle Order)
	for _, id := range ids {
		if s.redis != nil {
			key := helpers.GetRedisOrderKey(user.MerchantID, id)
			val, found := s.redis.Get(ctx, key)

			if found {
				var order models.Order
				errUnmarshal := json.Unmarshal([]byte(val), &order)

				if errUnmarshal == nil && order.OrderID != "" {
					log.Info("🧠🙋🏻‍♂️ Order found and parsed from Redis 🙋🏻‍♂️🧠 (key: " + key + ")")
					cacheResults[id] = order
					continue
				} else if errUnmarshal != nil {
					log.Warn("Failed to unmarshal Redis data, falling back to DB", zap.Error(errUnmarshal), zap.String("val", val))
				} else {
					log.Warn("Redis data unmarshaled to empty struct, falling back to DB", zap.String("val", val))
				}
			}
		}
		missingIDs = append(missingIDs, id)
	}

	// 3. Récupération DB pour les manquants
	if len(missingIDs) > 0 {
		dbOrders, err := s.GetOrdersByIDs(ctx, missingIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch missing orders from db: %w", err)
		}

		// 4. Stockage dans Redis (avec la MÊME clé structurée)
		for _, o := range dbOrders {
			cacheResults[o.OrderID] = o
			if s.redis != nil {
				// Utilisation exclusive du helper pour la clé !
				key := helpers.GetRedisOrderKey(user.MerchantID, o.OrderID)
				jsonData, _ := json.Marshal(o)
				_ = s.redis.Set(ctx, key, string(jsonData), 10*time.Minute) // J'ai aligné le TTL à 10 min, à adapter
				log.Info("🧠📌 Order saved in Redis cache 📌🧠 (key: " + key + ")")
			}
		}
	}

	// 5. Reconstruction dans l'ordre
	for _, id := range ids {
		if order, exists := cacheResults[id]; exists {
			finalOrders = append(finalOrders, order)
		}
	}

	return &models.PendingOrdersResponse{
		Orders: finalOrders,
	}, nil
}

func (s *OrdersService) ComputeGetOrder(ctx context.Context, merchantID, orderID string) (*models.PendingOrdersResponse, error) {
	key := helpers.GetRedisOrderKey(merchantID, orderID)
	log := logger.FromContext(ctx)

	// 1. TENTATIVE RAPIDE : On cherche un *models.Order* pur dans Redis
	if s.redis != nil {
		val, found := s.redis.Get(ctx, key)
		if found {
			var order models.Order
			if err := json.Unmarshal([]byte(val), &order); err == nil && order.OrderID != "" {
				log.Info("🧠🙋🏻‍♂️ Order found in Redis cache 🙋🏻‍♂️🧠 (key: " + key + ")")
				// On reconstruit le wrapper de réponse à la volée
				return &models.PendingOrdersResponse{
					Orders: []models.Order{order},
				}, nil
			}
		}
	}

	// 2. PROTECTION SINGLEFLIGHT
	res, err, _ := s.sfGroup.Do(key, func() (interface{}, error) {
		// 3. DOUBLE-CHECK Redis
		if s.redis != nil {
			valInner, foundInner := s.redis.Get(ctx, key)
			if foundInner {
				var orderInner models.Order
				if err := json.Unmarshal([]byte(valInner), &orderInner); err == nil && orderInner.OrderID != "" {
					log.Info("🧠🙋🏻‍♂️ Order found in Redis cache (SF) 🙋🏻‍♂️🧠 (key: " + key + ")")
					return &models.PendingOrdersResponse{
						Orders: []models.Order{orderInner},
					}, nil
				}
			}
		}

		// 4. APPEL RÉEL (DATABASE)
		resp, err := s.ordersRepo.GetOrder(ctx, merchantID, orderID)
		if err != nil {
			return nil, err
		}

		// 5. MISE EN CACHE DE L'ENTITÉ PURE : On extrait l'Order du wrapper pour le stocker
		if resp != nil && len(resp.Orders) > 0 && s.redis != nil {
			orderToCache := resp.Orders[0] // On isole la commande
			jsonData, _ := json.Marshal(orderToCache)
			_ = s.redis.Set(ctx, key, string(jsonData), 10*time.Minute)
			log.Info("🧠📌 Order saved in Redis cache 📌🧠 (key: " + key + ")")
		}

		return resp, nil
	})

	if err != nil {
		return nil, err
	}

	return res.(*models.PendingOrdersResponse), nil
}

func (s *OrdersService) GetOrder(ctx context.Context, orderID string) (*models.PendingOrdersResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.ComputeGetOrder(ctx, user.MerchantID, orderID)

}

func (s *OrdersService) GetOrders(ctx context.Context, req *models.OrderRequest) ([]models.Order, error) {
	// Récupérer l'utilisateur depuis le contexte
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.ordersRepo.GetOrders(ctx, user.MerchantID, req)
}

func (s *OrdersService) GetHistory(ctx context.Context, req models.OrderHistoryRequest) (*models.OrderHistoryData, error) {
	// Récupérer l'utilisateur depuis le contexte
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	orders, totalItems, totalRevenue, page, limit, err := s.ordersRepo.GetHistory(ctx, user.MerchantID, req)
	if err != nil {
		return nil, err
	}

	totalPages := 0
	if totalItems > 0 {
		totalPages = int(math.Ceil(float64(totalItems) / float64(limit)))
	}

	var avgBasket float64
	if totalItems > 0 {
		avgBasket = float64(totalRevenue) / float64(totalItems)
	}

	return &models.OrderHistoryData{
		Metadata: models.OrderHistoryMetadata{
			PaginationMetadata: models.PaginationMetadata{
				TotalItems:  totalItems,
				TotalPages:  totalPages,
				CurrentPage: page,
				Limit:       limit,
			},
			TotalRevenue: totalRevenue,
			AvgBasket:    avgBasket,
		},
		Orders: orders,
	}, nil
}

func (s *OrdersService) GetPayments(ctx context.Context, orderID string) ([]models.Payment, error) {
	// Récupérer l'utilisateur depuis le contexte (vérification d'authentification)
	_, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.ordersRepo.GetPaymentsForOrder(ctx, orderID)
}

/*
	func (s *OrdersService) DisablePayment(ctx context.Context, paymentID string) error {
		// Récupérer l'utilisateur depuis le contexte (vérification d'authentification)
		_, err := middleware.UserFromContext(ctx)
		if err != nil {
			return err
		}

		return s.ordersRepo.DisablePayment(ctx, paymentID)
	}

	func (s *OrdersService) SetDistributedProducts(ctx context.Context, req *models.SetDistributedProductsRequest) (map[string]interface{}, error) {
		user, err := middleware.UserFromContext(ctx)
		if err != nil {
			return nil, err
		}

		err = s.ordersRepo.SetDistributedProducts(ctx, user.UserID, user.MerchantID, req)
		if err != nil {
			return map[string]interface{}{
				"status": "-2",
				"error":  err.Error(),
			}, nil
		}

		return map[string]interface{}{"status": "1"}, nil
	}

	func (s *OrdersService) UpdateOrderOld(ctx context.Context, req *models.RequestObject) error {
		log := logger.FromContext(ctx)

		err := s.ordersRepo.UpdateOrder(ctx, req)

		if err != nil {
			log.Error(err.Error())
		} else {
			s.notificationsService.SendNotificationAsync(req.MerchantID, *req.Order.OrderID, notification.NotificationTypeOrderUpdate)
		}

		return nil
	}
*/
func (s *OrdersService) ComputePricing(ctx context.Context, req *models.PricingRequest) (*models.PricingResponse, error) {
	// --- Step 0: Init totals ---
	req.Order.TTC = 0
	req.Order.TVA = 0
	req.Order.HT = 0

	// --- Step 1: Load merchant info ---
	merchant, err := s.ordersRepo.GetMerchantPricingInfo(ctx, req.MerchantID)
	if err != nil || merchant == nil {
		return nil, err
	}

	// timezone conversion
	now := time.Now().UTC()
	loc, _ := time.LoadLocation(merchant.Timezone)
	merchantTime := now.In(loc)

	// 1 = lundi, ..., 7 = dimanche (1-7 standard)
	req.DayOfWeek = int(merchantTime.Weekday())
	if req.DayOfWeek == 0 {
		req.DayOfWeek = 7
	}
	req.Time = merchantTime.Format("2006-01-02 15:04:05")
	req.Order.Currency = merchant.Currency

	// --- Step 1bis: If no products ---
	if len(req.Order.Products) == 0 {
		/*
			return &models.PricingResponse{
				Status:       "success",
				OrderRequest: req,
			}, nil

		*/
	}

	// --- Step 2: Check product availability ---
	unavailable, err := s.ordersRepo.GetUnavailableProducts(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(unavailable) > 0 {
		return &models.PricingResponse{
			Status:             "success",
			OrderRequest:       req,
			UnavailableProduct: unavailable,
		}, nil
	}

	// --- Step 3: Load product prices + TVA ---
	dbProducts, err := s.ordersRepo.GetProductsForPricing(ctx, req)
	if err != nil {
		return nil, err
	}

	// --- Step 4: Expand products and compute base total ---
	selectedProducts, countProducts, baseTotal := s.buildSelectedProducts(req, dbProducts)

	// --- Step 5: Apply option price overrides ---
	if err := s.applyConfigurationOptionPrices(ctx, selectedProducts); err != nil {
		return nil, err
	}

	// --- Step 6: Load discount structures ---
	discounts, discountProducts, discountOptions, err := s.loadDiscountStructures(ctx, req)
	if err != nil {
		return nil, err
	}

	// --- Step 7: Apply discounts ---
	_ = s.applyDiscounts(req, selectedProducts, discounts, discountProducts, discountOptions, baseTotal)

	// --- Step 8: Load rewards ---
	rewards, err := s.ordersRepo.GetRewards(ctx, req)
	if err != nil {
		return nil, err
	}

	// --- Step 9: Apply rewards ---
	s.applyRewards(req, selectedProducts, rewards)

	// --- Step 10: Group identical products ---
	finalProducts := s.groupProducts(selectedProducts)

	// --- Step 11: Compute TTC/TVA/HT ---
	// On récupère le taux de TVA max ici
	maxTVARate := s.computeTotals(req, finalProducts)

	// --- Step 12: Apply delivery rules ---
	// On passe le taux max
	s.applyDeliveryRules(req, merchant, maxTVARate)

	// --- Step 13: Estimated distribution time ---
	estimatedTime, _ := s.ordersRepo.GetEstimatedDistributionTime(ctx, req, countProducts)

	// --- Final response ---
	return &models.PricingResponse{
		Status:                      "success",
		OrderRequest:                req,
		EstimatedDistributionTime:   estimatedTime,
		IsOrderable:                 req.IsOrderable,
		NotOrderableReason:          req.NotOrderableReason,
		MinimumCartForDeliveryOrder: float64(req.MinimumCartForDeliveryOrder),
	}, nil
}

func (s *OrdersService) GetPricing(ctx context.Context, req *models.PricingRequest) (*models.PricingResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.MerchantID = user.MerchantID

	return s.ComputePricing(ctx, req)
}

func (s *OrdersService) buildSelectedProducts(req *models.PricingRequest, dbProducts []models.DBProduct) ([]models.OrderProductPayload, int, int) {
	out := []models.OrderProductPayload{}
	total := 0
	count := 0

	// index products by id
	index := map[string]models.DBProduct{}
	for _, p := range dbProducts {
		index[p.ProductID] = p
	}

	orderType := strings.ToUpper(req.Order.OrderType)
	if orderType == "1" || orderType == "DELIVERY" {
		req.Order.OrderType = "DELIVERY"
	} else if orderType == "0" || orderType == "IN" {
		req.Order.OrderType = "IN"
	} else {
		req.Order.OrderType = "TAKE_AWAY"
	}

	for _, p := range req.Order.Products {
		dbp := index[p.ProductID]
		if p.Quantity <= 0 {
			continue
		}

		var price int
		var tva float64

		switch req.Order.OrderType {
		case "DELIVERY":
			price = dbp.PriceDelivery
			tva = dbp.TVARateDelivery
		case "TAKE_AWAY":
			price = dbp.PriceTakeAway
			tva = dbp.TVARateTakeAway
		default:
			price = dbp.Price
			tva = dbp.TVARateIn
		}

		for i := 0; i < p.Quantity; i++ {
			selected := models.OrderProductPayload{
				ProductID:       p.ProductID,
				ProductName:     dbp.Name,
				Quantity:        1,
				Comment:         p.Comment,
				Price:           price,
				TvaRate:         tva,
				Extra:           p.Extra,
				Without:         p.Without,
				Config:          p.Config,
				Description:     p.Description,
				DiscountID:      nil,
				DiscountedPrice: nil,
				OrderedDate:     req.Time,
				IsUpsell:        p.IsUpsell,
			}
			out = append(out, selected)
			count++
		}
		total += price * p.Quantity
	}

	return out, count, total
}

func (s *OrdersService) applyConfigurationOptionPrices(ctx context.Context, products []models.OrderProductPayload) error {

	// Collect option IDs
	optIDs := map[string]bool{}
	for _, p := range products {
		if p.Config == nil || p.Config.Attributes == nil {
			continue
		}
		for _, attr := range p.Config.Attributes {
			for _, opt := range attr.Options {
				optIDs[opt.ID] = true
			}
		}
	}

	if len(optIDs) == 0 {
		return nil
	}

	ids := make([]string, 0, len(optIDs))
	for id := range optIDs {
		ids = append(ids, id)
	}

	// Repo fetch
	priceMap, err := s.ordersRepo.GetConfigurationOptionPrices(ctx, ids)
	if err != nil {
		s.logPricingStageError(ctx, "load_configuration_option_prices", err,
			zap.Int("configuration_option_count", len(ids)),
		)
		return err
	}

	// Apply prices to products
	for i := range products {
		p := &products[i]

		if p.Config == nil || p.Config.Attributes == nil {
			continue
		}

		for _, attr := range p.Config.Attributes {
			for _, opt := range attr.Options {
				if val, ok := priceMap[opt.ID]; ok {
					opt.ExtraPrice = val
				}
			}
		}
	}

	return nil
}

func (s *OrdersService) loadDiscountStructures(ctx context.Context, req *models.PricingRequest) ([]*models.DBDiscount, map[string]map[string]*models.DiscountProductInfo, map[string]map[string][]models.DiscountOptionInfo, error) {
	pricingFields := []zap.Field{
		zap.String("merchant_id", req.MerchantID),
		zap.String("order_type", req.Order.OrderType),
		zap.Int("product_count", len(req.Order.Products)),
		zap.Bool("discount_code_present", req.DiscountCode != ""),
	}

	discounts, err := s.ordersRepo.GetDiscounts(ctx, req)
	if err != nil {
		s.logPricingStageError(ctx, "load_discounts", err, pricingFields...)
		return nil, nil, nil, err
	}

	dp, err := s.ordersRepo.GetDiscountProducts(ctx, req.MerchantID)
	if err != nil {
		s.logPricingStageError(ctx, "load_discount_products", err, pricingFields...)
		return nil, nil, nil, err
	}

	do, err := s.ordersRepo.GetDiscountProductOptions(ctx, req.MerchantID)
	if err != nil {
		s.logPricingStageError(ctx, "load_discount_product_options", err, pricingFields...)
		return nil, nil, nil, err
	}

	return discounts, dp, do, nil
}

func (s *OrdersService) applyDiscounts(req *models.PricingRequest, products []models.OrderProductPayload, discounts []*models.DBDiscount, dp map[string]map[string]*models.DiscountProductInfo, do map[string]map[string][]models.DiscountOptionInfo, baseTotal int) []string {

	applied := []string{}
	discountAlreadyApplied := false

	for _, d := range discounts {
		// --- 1. Filtres d'éligibilité globaux ---
		if d.DiscountOrderType != "" && !strings.Contains(d.DiscountOrderType, req.Order.OrderType) {
			continue
		}
		// Bug fix: une remise protégée par un code (discount_code non nul) ne doit jamais
		// s'appliquer sans saisie du code exact, sinon elle s'appliquait par défaut quand
		// req.DiscountCode était vide.
		if d.DiscountCode != nil {
			if req.DiscountCode == "" || req.DiscountCode != *d.DiscountCode {
				continue
			}
		}
		if discountAlreadyApplied && !d.IsCumulative {
			continue
		}

		relatedProducts := dp[d.DiscountID]
		relatedOptions := do[d.DiscountID]

		// --- 2. Comptage des produits éligibles (via Index pour éviter la copie) ---
		countEligible := 0
		for i := range products {
			if _, ok := relatedProducts[products[i].ProductID]; ok || len(relatedProducts) == 0 {
				if s.optionsMatch(products[i], relatedOptions[products[i].ProductID]) {
					countEligible++
				}
			}
		}

		if countEligible == 0 {
			continue
		}

		// --- 3. Vérification des conditions minimales ---
		testPassed := false
		switch d.MinOrderUnit {
		case "QTY":
			if countEligible >= d.MinOrderValue {
				testPassed = true
			}
		case "CURRENCY":
			if baseTotal >= d.MinOrderValue {
				testPassed = true
			}
		default:
			testPassed = true
		}

		if !testPassed {
			continue
		}

		// --- 4. Détermination du nombre d'applications ---
		n := d.DiscountedQuantity
		if d.IsCumulative {
			// Si cumulable, on applique la remise par paliers (ex: tous les 2 produits)
			n = countEligible - (countEligible % d.DiscountedQuantity)
		}

		// --- 5. Application de la remise ---
		appliedInThisLoop := 0
		for j := 0; j < n; j++ {
			found := false
			// On parcourt le slice original par index pour modifier les valeurs réelles
			for i := range products {
				sp := &products[i] // Pointeur sur l'élément du slice

				// On vérifie si le produit n'a pas déjà une remise et s'il correspond aux critères
				if sp.DiscountedPrice == nil && (len(relatedProducts) == 0 || relatedProducts[sp.ProductID] != nil) {
					if !s.optionsMatch(*sp, relatedOptions[sp.ProductID]) {
						continue
					}

					var calculatedPrice int
					switch d.DiscountUnit {
					case "PERCENTAGE":
						// Utilisation de float64 pour éviter la division entière (50/100 = 0)
						ratio := float64(d.DiscountValue) / 100.0
						calculatedPrice = int(float64(sp.Price) * (1.0 - ratio))

					case "CURRENCY":
						calculatedPrice = sp.Price - d.DiscountValue

					case "NEWPRICE":
						if prodInfo, ok := relatedProducts[sp.ProductID]; ok {
							calculatedPrice = prodInfo.NewPrice
						} else {
							calculatedPrice = sp.Price
						}
					}

					// Bug fix: plafonner le montant de la remise par MaxDiscountValue/MaxDiscountUnit
					// (jusqu'ici uniquement appliqué aux rewards, jamais aux discounts produit).
					if d.MaxDiscountValue != nil && *d.MaxDiscountValue > 0 {
						discountAmount := sp.Price - calculatedPrice
						var maxDiscount int
						if d.MaxDiscountUnit != nil && *d.MaxDiscountUnit == "PERCENTAGE" {
							maxDiscount = int(float64(sp.Price) * (*d.MaxDiscountValue) / 100.0)
						} else {
							maxDiscount = int(*d.MaxDiscountValue)
						}
						if discountAmount > maxDiscount {
							calculatedPrice = sp.Price - maxDiscount
						}
					}

					// Bug fix: un prix unitaire remisé ne doit jamais être négatif (Stripe rejette unit_amount < 0).
					calculatedPrice = int(math.Max(0, float64(calculatedPrice)))

					// Sécurisation des pointeurs : on crée de nouvelles adresses mémoire
					// pour que chaque produit ait sa propre instance de prix et d'ID
					finalPrice := calculatedPrice
					finalID := d.DiscountID
					finalName := d.DiscountName

					sp.DiscountedPrice = &finalPrice
					sp.DiscountID = &finalID
					sp.DiscountName = finalName

					applied = append(applied, d.DiscountID)
					discountAlreadyApplied = true
					appliedInThisLoop++
					found = true
					break // On passe au "j" suivant (prochaine application de la remise)
				}
			}
			// Si on n'a plus de produits éligibles dans cette boucle, on arrête
			if !found {
				break
			}
		}

		// Si une remise non-cumulable a été appliquée, on stoppe le traitement des autres remises
		if discountAlreadyApplied && !d.IsCumulative {
			break
		}
	}

	return applied
}

// optionsMatch vérifie si un produit (sp) satisfait les contraintes d'options d'une promotion.
// Si `promoOptions` est nil ou vide, on considère que la promotion n'impose rien -> true.
// Si le produit n'a pas de configuration, on renvoie true (rien à vérifier).
func (s *OrdersService) optionsMatch(sp models.OrderProductPayload, promoOptions []models.DiscountOptionInfo) bool {
	// rien à vérifier
	if len(promoOptions) == 0 || sp.Config == nil || len(sp.Config.Attributes) == 0 {
		return true
	}

	// construire set des options sélectionnées (quantity > 0 ou Selected)
	selected := make(map[string]bool)
	for _, attr := range sp.Config.Attributes {
		for _, opt := range attr.Options {
			// on considère sélection si Quantity > 0 ou Selected == true
			if opt.Quantity > 0 || opt.Selected {
				selected[opt.ID] = true
			}
		}
	}

	// vérifier uniquement les options marquées comme obligatoires dans promoOptions
	for _, req := range promoOptions {
		if req.IsOptionMandatory {
			if _, ok := selected[req.OptionID]; !ok {
				return false
			}
		}
	}

	return true
}

// isProductInList : utilitaire simple pour vérifier si un productID est présent dans une slice d'IDs.
func (s *OrdersService) isProductInList(productID string, list []string) bool {
	if len(list) == 0 {
		return false
	}
	for _, id := range list {
		if id == productID {
			return true
		}
	}
	return false
}

func (s *OrdersService) generateProductKey(p models.OrderProductPayload) string {

	extraJSON, _ := json.Marshal(p.Extra)
	withoutJSON, _ := json.Marshal(p.Without)
	configJSON, _ := json.Marshal(p.Config)

	discountID := ""
	if p.DiscountID != nil {
		discountID = *p.DiscountID
	}

	raw := p.ProductID +
		discountID +
		string(extraJSON) +
		string(withoutJSON) +
		string(configJSON) +
		strconv.FormatBool(p.IsUpsell)

	//hash := md5.Sum([]byte(raw))

	return raw //hex.EncodeToString(hash[:])
}

func (s *OrdersService) applyDiscountedOptionsPrice(product *models.SelectedProduct, discount *models.DBDiscount) {

	// aucune configuration
	if product.Config == nil ||
		len(product.Config.Attributes) == 0 ||
		discount.RelatedProductOptions == nil {
		return
	}

	productID := product.ProductID

	// promo ne concerne pas ce produit
	optList, ok := discount.RelatedProductOptions[productID]
	if !ok {
		return
	}

	// mapping option_id → new_price
	priceMap := make(map[string]int)

	for _, opt := range optList {
		if opt.NewPrice != nil {
			priceMap[opt.OptionID] = *opt.NewPrice
		}
	}

	if len(priceMap) == 0 {
		return
	}

	// appliquer les prix promo aux options configurées
	for _, attr := range product.Config.Attributes {
		for _, opt := range attr.Options {
			if np, ok := priceMap[opt.ID]; ok {
				opt.ExtraPrice = np
			}
		}
	}
}

func (s *OrdersService) optionsMatchPromotion(product *models.SelectedProduct, promoOptions map[string][]models.DiscountOptionInfo) bool {

	// aucun contrôle à faire
	if product.Config == nil ||
		product.Config.Attributes == nil {
		return true
	}

	required, ok := promoOptions[product.ProductID]
	if !ok {
		return true
	}

	// extraire les options sélectionnées (quantity > 0)
	selected := map[string]bool{}

	for _, attr := range product.Config.Attributes {
		for _, opt := range attr.Options {
			if opt.Quantity > 0 {
				selected[opt.ID] = true
			}
		}
	}

	// vérifier les options obligatoires
	for _, reqOpt := range required {
		if reqOpt.IsOptionMandatory {
			if !selected[reqOpt.OptionID] {
				return false
			}
		}
	}

	return true
}

func (s *OrdersService) applyRewards(req *models.PricingRequest, products []models.OrderProductPayload, rewards []*models.DBReward) {
	req.Order.UsedRewards = []*models.UsedReward{}
	rewardBaseAmount := s.computeRewardBaseAmount(products)
	if rewardBaseAmount <= 0 {
		return
	}

	rewardPaymentAmount := 0
	usedRewardsCount := 0

	for _, rwd := range rewards {
		if rwd.RewardValue == nil || *rwd.RewardValue <= 0 {
			continue
		}

		if rwd.RewardOrderType != "" && !strings.Contains(rwd.RewardOrderType, req.Order.OrderType) {
			continue
		}

		if rwd.MinOrderValue > 0 && rewardBaseAmount < rwd.MinOrderValue {
			continue
		}

		if rwd.MaxRewardsPerOrder > 0 && usedRewardsCount >= rwd.MaxRewardsPerOrder {
			continue
		}

		switch rwd.RewardType {
		case "free_product":
			applied := false
			for i := range products {
				sp := &products[i]
				if sp.DiscountedPrice == nil && s.isProductInList(sp.ProductID, rwd.ProductIDs) {
					val := rwd.RewardValue
					sp.DiscountedPrice = val
					sp.DiscountID = &rwd.RewardID

					req.Order.UsedRewards = append(req.Order.UsedRewards, &models.UsedReward{
						RewardID: rwd.RewardID,
					})
					usedRewardsCount++
					applied = true
					break
				}
			}
			if !applied {
				continue
			}

		case "fixed_discount":
			discountAmount := *rwd.RewardValue
			if rwd.MaxDiscountValue != nil && discountAmount > *rwd.MaxDiscountValue {
				discountAmount = *rwd.MaxDiscountValue
			}

			remaining := rewardBaseAmount - rewardPaymentAmount
			if remaining <= 0 {
				continue
			}
			if discountAmount > remaining {
				discountAmount = remaining
			}

			if discountAmount <= 0 {
				continue
			}

			rewardPaymentAmount += discountAmount
			usedRewardsCount++
			req.Order.UsedRewards = append(req.Order.UsedRewards, &models.UsedReward{RewardID: rwd.RewardID})

		case "percent_discount", "percentage_discount":
			discountAmount := (rewardBaseAmount * (*rwd.RewardValue)) / 100
			if rwd.MaxDiscountValue != nil && discountAmount > *rwd.MaxDiscountValue {
				discountAmount = *rwd.MaxDiscountValue
			}

			remaining := rewardBaseAmount - rewardPaymentAmount
			if remaining <= 0 {
				continue
			}
			if discountAmount > remaining {
				discountAmount = remaining
			}

			if discountAmount <= 0 {
				continue
			}

			rewardPaymentAmount += discountAmount
			usedRewardsCount++
			req.Order.UsedRewards = append(req.Order.UsedRewards, &models.UsedReward{RewardID: rwd.RewardID})
		}
	}

	if rewardPaymentAmount > 0 {
		for i := range req.Order.Payments {
			if req.Order.Payments[i].MOP == "REWARD" {
				req.Order.Payments[i].Amount += rewardPaymentAmount
				return
			}
		}

		req.Order.Payments = append(req.Order.Payments, models.PaymentPayload{
			Amount: rewardPaymentAmount,
			MOP:    "REWARD",
		})
	}
}

func (s *OrdersService) computeRewardBaseAmount(products []models.OrderProductPayload) int {
	total := 0

	for _, p := range products {
		unit := p.Price
		if p.DiscountedPrice != nil {
			unit = *p.DiscountedPrice
		}

		qty := p.Quantity
		if qty <= 0 {
			qty = 1
		}

		lineTotal := unit * qty

		for _, e := range p.Extra {
			lineTotal += e.Price * qty
		}

		if p.Config != nil && p.Config.Attributes != nil {
			for _, attr := range p.Config.Attributes {
				for _, opt := range attr.Options {
					optionQty := opt.Quantity
					if opt.Selected && optionQty == 0 {
						optionQty = 1
					}
					if optionQty > 0 {
						lineTotal += opt.ExtraPrice * optionQty * qty
					}
				}
			}
		}

		total += lineTotal
	}

	if total < 0 {
		return 0
	}

	return total
}

func (s *OrdersService) groupProducts(products []models.OrderProductPayload) []models.OrderProductPayload {
	out := []models.OrderProductPayload{}
	index := map[string]int{}

	for _, p := range products {
		key := s.generateProductKey(p)

		if idx, ok := index[key]; ok {
			out[idx].Quantity++
		} else {
			index[key] = len(out)
			out = append(out, p)
		}
	}
	return out
}

// Retourne le taux de TVA max trouvé (pour les frais de livraison)
func (s *OrdersService) computeTotals(req *models.PricingRequest, products []models.OrderProductPayload) float64 {
	req.Order.Products = products
	// Réinitialisation (Step 0 du PHP)
	req.Order.TTC = 0
	req.Order.TVA = 0
	req.Order.HT = 0

	var maxTVARate float64 = 0

	for i := range req.Order.Products {
		// Utilisation d'un pointeur pour pouvoir mettre à jour les sous-totaux dans le slice si besoin,
		// bien que ici on ne modifie que les totaux globaux.
		p := &req.Order.Products[i]

		// 1. Déterminer le prix unitaire TTC effectif
		unitTTC := p.Price
		if p.DiscountedPrice != nil {
			unitTTC = *p.DiscountedPrice
		}

		qty := p.Quantity
		tvaRate := p.TvaRate

		// Tracking du taux max pour la livraison
		if tvaRate > maxTVARate {
			maxTVARate = tvaRate
		}

		// --- CALCUL PRODUIT (Logique PHP) ---
		// $product_ttc = round($unit_ttc * $qty, 2);
		lineTTC := unitTTC * qty // Int * Int

		// $product_ht = round($product_ttc / (1 + $tvaRate / 100), 2);
		lineHT := helpers.RoundToNearestInt(float64(lineTTC) / (1 + tvaRate/100))

		// $product_tva = round($product_ttc - $product_ht, 2);
		lineTVA := lineTTC - lineHT

		req.Order.TTC += lineTTC
		req.Order.HT += lineHT
		req.Order.TVA += lineTVA

		// --- CALCUL EXTRAS ---
		if p.Extra != nil {
			for _, e := range p.Extra {
				if e.Price != 0 {
					extraTTC := e.Price * qty
					extraHT := helpers.RoundToNearestInt(float64(extraTTC) / (1 + tvaRate/100))
					extraTVA := extraTTC - extraHT

					req.Order.TTC += extraTTC
					req.Order.HT += extraHT
					req.Order.TVA += extraTVA
				}
			}
		}

		// --- CALCUL OPTIONS / CONFIG ---
		if p.Config != nil && p.Config.Attributes != nil {
			for _, a := range p.Config.Attributes {
				for _, o := range a.Options {
					if o.Selected && o.ExtraPrice != 0 {
						optTTC := o.ExtraPrice * qty
						optHT := helpers.RoundToNearestInt(float64(optTTC) / (1 + tvaRate/100))
						optTVA := optTTC - optHT

						req.Order.TTC += optTTC
						req.Order.HT += optHT
						req.Order.TVA += optTVA
					}
				}
			}
		}
	}

	return maxTVARate
}

func (s *OrdersService) applyDeliveryRules(req *models.PricingRequest, merchant *models.MerchantPricingInfo, maxTVARate float64) {
	req.MinimumCartForDeliveryOrder = merchant.MinimumCartForDeliveryOrder
	req.IsOrderable = true

	// Règle d'éligibilité (Minimum de commande)
	if req.Order.OrderType == "DELIVERY" &&
		req.IsSNO && // Supposons que IsSNO est défini ailleurs ou passé dans le contexte
		req.Order.TTC < merchant.MinimumCartForDeliveryOrder {
		req.IsOrderable = false
		req.NotOrderableReason = "minimum_cart_for_delivery_not_reached"
	}

	// Calcul des frais (Logique PHP)
	// if ($order_type === "DELIVERY" && $TTC < $limit)
	if req.Order.OrderType == "DELIVERY" && req.Order.TTC < merchant.DeliveryFeesLimit {

		deliveryTTC := merchant.DeliveryFees

		// Calcul HT/TVA sur la livraison basé sur le taux max du panier
		// $delivery_ht = round($delivery_ttc / (1 + $delivery_tva_rate / 100), 2);
		deliveryHT := helpers.RoundToNearestInt(float64(deliveryTTC) / (1 + maxTVARate/100))
		deliveryTVA := deliveryTTC - deliveryHT

		req.Order.DeliveryFees = deliveryTTC

		req.Order.TTC += deliveryTTC
		req.Order.HT += deliveryHT
		req.Order.TVA += deliveryTVA

	} else {
		req.Order.DeliveryFees = 0
	}
}
