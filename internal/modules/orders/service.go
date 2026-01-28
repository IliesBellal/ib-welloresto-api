package orders

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/notification"
)

type OrdersService struct {
	ordersRepo           *OrdersRepository
	userRepo             auth.AuthService
	notificationsService *notification.NotificationService
}

func NewOrdersService(ordersRepo *OrdersRepository,
	userRepo auth.AuthService, notificationsService *notification.NotificationService) *OrdersService {
	return &OrdersService{
		ordersRepo:           ordersRepo,
		userRepo:             userRepo,
		notificationsService: notificationsService,
	}
}

func (s *OrdersService) ReopenClosedOrder(ctx context.Context, token, orderID string) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("invalid token")
	}

	// Ici user.MerchantID et user.UserID sont récupérés automatiquement

	return s.ordersRepo.ReopenClosedOrder(ctx, user.MerchantID, orderID, user.UserID)
}

func (s *OrdersService) AddPayment(ctx context.Context, token string, orderID string, req *models.PaymentRequest) error {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return fmt.Errorf("invalid token")
	}

	// sécurité : orderID dans l’URL > orderID dans req
	req.OrderID = orderID

	return s.ordersRepo.AddPayment(ctx, user.MerchantID, user.UserID, req)
}

func (s *OrdersService) GetPendingOrders(ctx context.Context, token string, app string) (*models.PendingOrdersResponse, error) {
	// Resolve user by token to get merchant id
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	return s.ordersRepo.GetPendingOrders(ctx, user.MerchantID, app)
}

func (s *OrdersService) GetOrder(ctx context.Context, token, orderID string) (*models.Order, error) {
	// Resolve user by token to get merchant id
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	return s.ordersRepo.GetOrder(ctx, user.MerchantID, orderID)
}

func (s *OrdersService) GetOrders(ctx context.Context, token string, req *models.OrderRequest) ([]models.Order, error) {
	// Resolve user by token to get merchant id
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	return s.ordersRepo.GetOrders(ctx, user.MerchantID, req)
}

func (s *OrdersService) UpdateMultipleProductsStatus(ctx context.Context, req *models.MultipleProductsRequest) error {

	return s.ordersRepo.UpdateMultipleProductsStatus(ctx, req)
}

func (s *OrdersService) GetHistory(ctx context.Context, token string, req models.OrderHistoryRequest) ([]models.Order, error) {
	// Resolve user by token to get merchant id
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	return s.ordersRepo.GetHistory(ctx, user.MerchantID, req)
}

func (s *OrdersService) GetPayments(ctx context.Context, token string, orderID string) ([]models.Payment, error) {
	// Resolve user by token to get merchant id
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}
	return s.ordersRepo.GetPaymentsForOrder(ctx, orderID)
}

func (s *OrdersService) DisablePayment(ctx context.Context, token string, paymentID string) error {
	// Resolve user by token to get merchant id
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("invalid token")
	}

	return s.ordersRepo.DisablePayment(ctx, paymentID)
}

func (s *OrdersService) SetDistributedProducts(ctx context.Context, token string, req *models.SetDistributedProductsRequest) (map[string]interface{}, error) {

	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil || user == nil {
		return map[string]interface{}{"status": "-1", "error": "Invalid token"}, nil
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

func (s *OrdersService) CreateOrder(ctx context.Context, token string, req *models.RequestObject) (*models.CreateOrderResult, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	req.MerchantID = user.MerchantID
	req.Order.CreatedBy = &user.UserID

	log := logger.FromContext(ctx)

	result, err := s.ordersRepo.CreateOrder(ctx, req)

	if err != nil {
		log.Error(err.Error())
	} else {
		log.Warn("New order created : " + result.OrderID)
		s.notificationsService.SendNotificationAsync(user.MerchantID, result.OrderID, "NEW_ORDER")
	}
	return result, err
}

func (s *OrdersService) GetPricing(ctx context.Context, token string, req *models.PricingRequest) (*models.PricingResponse, error) {
	user, err := s.userRepo.GetUserByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid token")
	}

	req.MerchantID = user.MerchantID

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

	req.DayOfWeek = int(merchantTime.Weekday())
	if req.DayOfWeek == 0 {
		req.DayOfWeek = 7
	}
	req.Time = merchantTime.Format("2006-01-02 15:04:05")
	req.Order.Currency = merchant.Currency

	// --- Step 1bis: If no products ---
	if len(req.Order.Products) == 0 {
		return &models.PricingResponse{
			Status:       1,
			OrderRequest: req,
		}, nil
	}

	// --- Step 2: Check product availability ---
	unavailable, err := s.ordersRepo.GetUnavailableProducts(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(unavailable) > 0 {
		return &models.PricingResponse{
			Status:             1,
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
	appliedDiscounts := s.applyDiscounts(req, selectedProducts, discounts, discountProducts, discountOptions, baseTotal)

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
		Status:                    1,
		OrderRequest:              req,
		EstimatedDistributionTime: estimatedTime,
		AppliedDiscounts:          appliedDiscounts,
	}, nil
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

	discounts, err := s.ordersRepo.GetDiscounts(ctx, req)
	if err != nil {
		return nil, nil, nil, err
	}

	dp, err := s.ordersRepo.GetDiscountProducts(ctx, req.MerchantID)
	if err != nil {
		return nil, nil, nil, err
	}

	do, err := s.ordersRepo.GetDiscountProductOptions(ctx, req.MerchantID)
	if err != nil {
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
		if req.DiscountCode != "" && d.DiscountCode != nil && *d.DiscountCode != req.DiscountCode {
			continue
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

					// Sécurisation des pointeurs : on crée de nouvelles adresses mémoire
					// pour que chaque produit ait sa propre instance de prix et d'ID
					finalPrice := calculatedPrice
					finalID := d.DiscountID

					sp.DiscountedPrice = &finalPrice
					sp.DiscountID = &finalID

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

	raw := fmt.Sprintf("%d|%v|%s|%s|%s",
		p.ProductID,
		p.DiscountID,
		string(extraJSON),
		string(withoutJSON),
		string(configJSON),
	)

	hash := md5.Sum([]byte(raw))
	return hex.EncodeToString(hash[:])
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

	for _, rwd := range rewards {

		if rwd.RewardOrderType != "" && !strings.Contains(rwd.RewardOrderType, req.Order.OrderType) {
			continue
		}

		switch rwd.RewardType {
		case "free_product":
			for _, sp := range products {
				if sp.DiscountedPrice == nil && s.isProductInList(sp.ProductID, rwd.ProductIDs) {
					val := rwd.RewardValue
					sp.DiscountedPrice = val
					sp.DiscountID = &rwd.RewardID

					req.Order.UsedRewards = append(req.Order.UsedRewards, &models.UsedReward{
						RewardID: rwd.RewardID,
					})
					break
				}
			}
		}
	}
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
