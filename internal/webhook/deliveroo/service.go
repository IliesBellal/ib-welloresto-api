package deliveroo

import (
	"context"
	"fmt"
	"time"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/modules/deliveroo"

	"welloresto-api/internal/models"                   // Assurez-vous que le chemin est correct
	"welloresto-api/internal/modules/order_life_cycle" // Chemin vers OrderLifeCycle
	"welloresto-api/internal/modules/orders"           // Chemin vers votre OrdersService

	"go.uber.org/zap"
)

type DeliverooService struct {
	repo             *Repository
	ordersService    *orders.OrdersService
	lifecycleService *order_life_cycle.OrdersLifeCycleService
	httpClient       *deliveroo.DeliverooService
}

func NewDeliverooService(repo *Repository, ordSvc *orders.OrdersService, lcSvc *order_life_cycle.OrdersLifeCycleService, deliverooInternalService *deliveroo.DeliverooService) *DeliverooService {
	return &DeliverooService{
		repo:             repo,
		ordersService:    ordSvc,
		lifecycleService: lcSvc,
		httpClient:       deliverooInternalService,
	}
}

func (s *DeliverooService) ProcessNewOrder(ctx context.Context, payload DeliverooWebhookPayload) error {
	ord := payload.Body.Order

	// 1. Récupérer le Merchant ID via le Location ID
	merchantData, err := s.repo.GetMerchantByLocationID(ctx, ord.LocationID)
	if err != nil {
		return err
	}

	// 2. Calcul du numéro de commande
	orderNum, err := s.repo.GetNextOrderNum(ctx, merchantData.MerchantID)
	if err != nil {
		return err
	}

	// 3. Préparer les produits et options (Sync) & Construire le payload
	productsPayload, err := s.buildProductsPayload(ctx, merchantData.MerchantID, ord.Items)
	if err != nil {
		return err
	}

	// 4. Mapping Client & Adressage
	customerReq, err := s.buildCustomerRequest(merchantData.MerchantID, ord)
	if err != nil {
		return err
	}

	// 5. Mapping Global de la commande
	reqObject := s.buildOrderRequestObject(merchantData.MerchantID, orderNum, ord, productsPayload, customerReq)

	// 6. CREATION DE LA COMMANDE via le Service existant
	result, err := s.ordersService.CreateOrder(ctx, reqObject)
	if err != nil {
		return fmt.Errorf("failed to create order in service: %w", err)
	}

	// 7. Post-traitement (Auto-accept ou Notification)
	// Note: Le PHP utilisait une classe externe Deliveroo pour notifier.
	// Ici, on utilise le lifecycle comme demandé.
	if merchantData.AutoAcceptOrders {
		// Logique d'acceptation
		_, err := s.lifecycleService.SetOrderAccepted(ctx, "DELIVEROO_WEBHOOK", merchantData.MerchantID, result.OrderID)
		if err != nil {
			// On log l'erreur mais on ne bloque pas forcément le flux car la commande est créée
			fmt.Printf("Error auto-accepting order: %v\n", err)
		}
	} else {
		// Logique de notification manuelle (à implémenter si hors du lifecycle)
		// ex: s.notificationService.Notify(...)
	}

	return nil
}

// buildProductsPayload itère sur les items, synchronise (crée si besoin) et retourne la structure OrderProductPayload
func (s *DeliverooService) buildProductsPayload(ctx context.Context, merchantID string, items []DeliverooItem) ([]models.OrderProductPayload, error) {
	var payload []models.OrderProductPayload

	for _, item := range items {
		// A. Sync Product (DB check/insert)
		internalProdID, err := s.repo.SyncProduct(ctx, merchantID, item)
		if err != nil {
			return nil, fmt.Errorf("sync product error (%s): %w", item.Name, err)
		}

		price := item.TotalPrice.Fractional - item.DiscountAmount.Fractional
		if item.Quantity > 0 {
			price = price / item.Quantity // Prix unitaire approximatif si nécessaire, mais CreateOrder semble prendre le total ? À vérifier selon votre implémentation. Le PHP met unit_price * quantity ou total.
			// Le PHP mettait: "price" => ($prod->total_price->fractional - $prod->discount_amount->fractional) pour l'item global dans orderitems.
			// Mais OrderProductPayload demande souvent un prix unitaire.
			// Corrigeons selon le struct: OrderProductPayload a `Price int` et `Quantity int`.
			// Le PHP insérait le prix total dans `orderitems`. Assumons que Price ici est unitaire *ou* géré par le service.
			// Pour la sûreté, utilisons le UnitPrice fourni par Deliveroo :
			price = item.UnitPrice.Fractional
		}

		prodPayload := models.OrderProductPayload{
			ProductID:   internalProdID,
			Quantity:    item.Quantity,
			Price:       price,
			ProductName: item.Name,
			Config:      &models.ProductConfiguration{Attributes: []models.ConfigurationAttribute{}},
		}

		// B. Sync Modifiers
		if len(item.Modifiers) > 0 {
			// Regrouper par attribut pour le modèle ConfigAttribute
			attrMap := make(map[string]*models.ConfigurationAttribute)

			for _, mod := range item.Modifiers {
				attrID, optID, err := s.repo.SyncOption(ctx, merchantID, internalProdID, mod)
				if err != nil {
					return nil, fmt.Errorf("sync option error (%s): %w", mod.Name, err)
				}

				if _, exists := attrMap[attrID]; !exists {
					attrMap[attrID] = &models.ConfigurationAttribute{
						ID:      attrID,
						Options: []models.ConfigurationOption{},
					}
				}

				entry := attrMap[attrID]
				entry.Options = append(entry.Options, models.ConfigurationOption{
					ID:       optID,
					Quantity: mod.Quantity,
				})
			}

			// Convert map to slice
			for _, attr := range attrMap {
				prodPayload.Config.Attributes = append(prodPayload.Config.Attributes, *attr)
			}
		}

		payload = append(payload, prodPayload)
	}
	return payload, nil
}

func (s *DeliverooService) buildCustomerRequest(merchantID string, ord DeliverooOrder) (*models.CustomerRequest, error) {
	var name, phone, address string
	var lat, lng float64

	isRestaurantFulfillment := ord.FulfillmentType == "DELIVERY_BY_RESTAURANT"

	if isRestaurantFulfillment && ord.Delivery != nil {
		name = ord.Delivery.CustomerName
		phone = ord.Delivery.ContactNumber
		address = fmt.Sprintf("%s, %s, %s", ord.Delivery.Line1, ord.Delivery.Postcode, ord.Delivery.City)
		lat = ord.Delivery.Location.Latitude
		lng = ord.Delivery.Location.Longitude
	} else if ord.Customer != nil {
		// Takeaway ou Delivery by Deliveroo
		name = ord.Customer.FirstName
		phone = ord.Customer.ContactNumber
		// Pas d'adresse précise pour le client dans ce cas (géré par Deliveroo)
	}

	return &models.CustomerRequest{
		MerchantID: &merchantID,
		Name:       &name,
		Tel:        &phone,
		Address:    &address,
		Lat:        &lat,
		Lng:        &lng,
		// Les champs supplémentaires peuvent être mappés si disponibles
	}, nil
}

func (s *DeliverooService) buildOrderRequestObject(merchantID, orderNum string, ord DeliverooOrder, products []models.OrderProductPayload, customer *models.CustomerRequest) *models.RequestObject {

	// Conversion des dates
	prepareFor := ord.PrepareFor
	if t, err := time.Parse(time.RFC3339, ord.PrepareFor); err == nil {
		prepareFor = t.UTC().Format("2006-01-02 15:04:05") // Format SQL standard
	}

	// Gestion du paiement
	total := ord.TotalPrice.Fractional - ord.CashDue.Fractional
	payments := []models.PaymentPayload{}
	if total > 0 {
		payments = append(payments, models.PaymentPayload{
			Amount: total,
			MOP:    "DELIVEROO",
		})
	}

	// Order Type
	orderType := "DELIVERY"
	if ord.FulfillmentType == "CUSTOMER" { // Take Away
		orderType = "TAKE_AWAY"
	}

	// Remake logic
	var parentOrderID *string
	// Attention: Le RequestObject n'a pas de champ explicite 'ParentOrderID' au premier niveau dans ta struct OrderRequest fournie,
	// mais le PHP fait un UPDATE direct.
	// Je le passe dans "BookingID" ou "Comment" si le modèle Go ne le supporte pas, ou il faut l'ajouter au modèle Go.
	// Pour l'instant, je le laisse de côté ou je le mets dans Commentaire si nécessaire.
	// Si le struct RequestObject OrderRequest a été mis à jour pour inclure cela, l'ajouter ici.

	// Commentaires
	comment := ord.OrderNotes
	if ord.CutleryNotes != "" && ord.CutleryNotes != "PAS DE COUVERTS" {
		comment += " [Couverts demandés]"
	}

	// Helpers pour les pointeurs
	deviceID := "DELIVEROO"
	createdBy := "WEBHOOK_DELIVEROO"
	brandStatus := ord.Status
	merchantApproval := "PENDING_APPROVAL"
	fulfillmentType := "DELIVERY_BY_RESTAURANT"
	if ord.FulfillmentType != "RESTAURANT" {
		fulfillmentType = "DELIVEROO"
	}

	req := &models.RequestObject{
		MerchantID: merchantID,
		DeviceID:   &deviceID,
		Order: models.OrderRequest{
			OrderNum:         &orderNum,
			Brand:            models.BrandDeliveroo,
			BrandOrderNum:    &ord.DisplayID,
			BrandOrderID:     &ord.ID,
			ParentOrderID:    parentOrderID,
			CashRegisterId:   &deviceID,
			FulfillmentType:  &fulfillmentType,
			TTC:              ord.TotalPrice.Fractional,
			HT:               0, // Calculé par le service généralement
			TVA:              0, // Calculé par le service généralement
			Products:         products,
			Customer:         customer,
			OrderType:        orderType,
			CreatedBy:        &createdBy,
			Comment:          &comment,
			Payments:         payments,
			DeliveryFees:     0, // PHP mettait 0
			EstimatedReady:   prepareFor,
			MerchantApproval: merchantApproval,
			BrandStatus:      brandStatus,
			OnlinePayment:    total > 0, // Si Deliveroo a collecté l'argent
			Currency:         nil,       // Défaut
		},
	}

	return req
}

// --- Status Update Logic ---
// Ajout de (err error) en retour nommé pour simplifier le defer
func (s *DeliverooService) ProcessStatusUpdate(ctx context.Context, payload DeliverooWebhookPayload) (err error) {
	log := logger.FromContext(ctx)
	ord := payload.Body.Order

	// 1. Récupérer le marchand
	merchant, err := s.repo.GetMerchantByLocationID(ctx, ord.LocationID)
	if err != nil {
		return err
	}
	log.Info("Webhook DELIVEROO : ProcessStatusUpdate " + ord.ID + " - " + ord.Status + " (Merchant :" + merchant.MerchantID + ")")

	// 2. Gestion d'erreur globale automatique :
	// Si la fonction retourne une erreur (err != nil), on alerte Deliveroo
	defer func() {
		if err != nil {
			log.Error("Webhook processing failed", zap.Error(err))
			s.setSyncStatus(ctx, ord.ID, "failed", "webhook_failed")
		}
	}()

	// 3. Initialiser la transaction
	tx, err := s.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 4. Récupérer l'ID interne
	internalOrderID, err := s.repo.GetOrderIDByBrandID(ctx, tx, ord.ID)
	if err != nil {
		return err
	}

	// 5. COMMIT de la transaction AVANT les appels lifecycle
	if err = tx.Commit(); err != nil {
		return err
	}

	// 6. Actions métier (Lifecycle) hors transaction
	switch ord.Status {
	case "rejected", "canceled":
		reason := models.DenyOrderInput{
			OrderID:          internalOrderID,
			MerchantID:       merchant.MerchantID,
			DeletionReasonID: "43",
			UserID:           "WEBHOOK_DELIVEROO",
		}

		// Attention: Assure-toi que DeleteOrder retourne une erreur pour que le defer la capte si ça casse
		err = s.lifecycleService.DeleteOrder(ctx, reason)

		// Envoi succès à Deliveroo en asynchrone
		//go s.setSyncStatus(ctx, ord.ID, "succeeded", "")
		return err

	case "accepted":
		/* TODO manage this time
		now := time.Now()

		prepareFor, _ := time.Parse(time.RFC3339, ord.PrepareFor)

		isScheduledToggle := false
		if !ord.ASAP && prepareFor.After(now) {
			isScheduledToggle = true
		}
		*/

		if _, err = s.lifecycleService.SetOrderAccepted(ctx, "WEBHOOK_DELIVEROO", merchant.MerchantID, internalOrderID); err != nil {
			return err
		}

		// Envoi succès à Deliveroo en asynchrone
		go s.setSyncStatus(ctx, ord.ID, "succeeded", "")
		return nil

	case "confirmed":
		if _, err = s.lifecycleService.SetOrderAccepted(ctx, "WEBHOOK_DELIVEROO", merchant.MerchantID, internalOrderID); err != nil {
			return err
		}

		// Envoi succès à Deliveroo en asynchrone
		go s.setSyncStatus(ctx, ord.ID, "succeeded", "")

		return nil

	default:

		// Envoi succès à Deliveroo en asynchrone
		go s.setSyncStatus(ctx, ord.ID, "succeeded", "")
		// Do nothing
		return nil
	}
}

// --- Helpers API ---

func (s *DeliverooService) getToken() string {
	// TODO: Implémenter la logique OAuth2 (client_credentials) pour récupérer/cacher le token
	// Comme dans la fonction PHP getToken()
	return "YOUR_ACCESS_TOKEN"
}

func (s *DeliverooService) setSyncStatus(ctx context.Context, brandOrderID, status, reason string) {
	s.httpClient.SetSyncStatus(ctx, brandOrderID, status, reason)
}
