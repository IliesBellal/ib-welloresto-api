package service

import (
	"context"
	"database/sql"
	"net/http"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/modules/googlemaps"
	"welloresto-api/internal/modules/menu"
	"welloresto-api/internal/modules/notification"

	models "welloresto-api/internal/models"
	"welloresto-api/internal/modules/order_life_cycle"
	"welloresto-api/internal/modules/orders"
	"welloresto-api/internal/modules/ubereats"
	"welloresto-api/internal/utils/dbutils"
	"welloresto-api/internal/webhook/idempotency"
	"welloresto-api/internal/webhook/marketplacefallback"
	ueClient "welloresto-api/internal/webhook/ubereats/client"

	"go.uber.org/zap"
	modelsUber "welloresto-api/internal/webhook/ubereats/models"
	"welloresto-api/internal/webhook/ubereats/repository"
)

type Service struct {
	signatureSecret string

	productMappingRepo   ProductMappingRepository
	attributeMappingRepo AttributeMappingRepository

	googleClient         *googlemaps.GoogleMapsClient
	uberClient           *ubereats.UberEatsService
	ordersService        *orders.OrdersService
	menuService          productCreator
	ordersRepo           *repository.OrdersRepository
	orderLifeCycleSvc    *order_life_cycle.OrdersLifeCycleService
	notificationsService *notification.NotificationService
	db                   *sql.DB
	redis                *redis.Client // Optionnel, pour le caching

	// inLockedTx exécute fn dans une transaction qui tient le verrou nommé key :
	// les « trouver ou créer » de mapping (produit, groupe, option) ne se font
	// qu'un à la fois, voir dbutils.RunInTxLocked. Remplaçable en test.
	inLockedTx func(ctx context.Context, key string, fn func(txCtx context.Context) error) error
	// fallbackProduct renvoie l'article de repli de l'établissement, voir
	// marketplacefallback. Remplaçable en test.
	fallbackProduct func(ctx context.Context, merchantID string) (string, error)
}

// productCreator crée le produit d'un article Uber Eats inconnu (menu.MenuService).
type productCreator interface {
	CreateProductFromExternal(ctx context.Context, merchantID, title, description string, amount int, alcohol bool) (*string, error)
}

type ProductMappingRepository interface {
	FindProductIDByUberItemID(ctx context.Context, merchantID, uberItemID string) (*string, error)
	CreateProductMapping(ctx context.Context, merchantID, productID, uberItemID string) error
}

type CatalogService interface {
	CreateProductFromExternal(ctx context.Context, tx *sql.Tx, merchantID, name, desc string, price int) (string, error)
}

type AttributeMappingRepository interface {
	GetAttributeIDByModifierGroupID(ctx context.Context, merchantID, groupID string) (*string, error)
	CreateAttributeFromUberGroup(ctx context.Context, merchantID, name, productID string) (string, error)
	CreateAttributeMapping(ctx context.Context, merchantID, attrID, groupID string) error

	GetOptionIDByUberItemID(ctx context.Context, attributeID, uberItemID string) (*string, error)
	CreateOptionFromUber(ctx context.Context, attributeID, title string, price int) (string, error)
	CreateOptionMapping(ctx context.Context, merchantID, optionID, uberItemID string) error
}

func NewService(
	db *sql.DB,
	signatureSecret string,
	uberClient *ubereats.UberEatsService,
	googleClient *googlemaps.GoogleMapsClient,
	ordersService *orders.OrdersService,
	menuService *menu.MenuService,
	orderLifeCycleSvc *order_life_cycle.OrdersLifeCycleService,
	notificationService *notification.NotificationService,
	redis *redis.Client,
) *Service {
	return &Service{
		db:                   db,
		signatureSecret:      signatureSecret,
		uberClient:           uberClient,
		googleClient:         googleClient,
		ordersService:        ordersService,
		productMappingRepo:   repository.NewProductMappingRepository(db),
		attributeMappingRepo: repository.NewAttributeMappingRepository(db),
		menuService:          menuService,
		ordersRepo:           repository.NewOrdersRepository(db),
		orderLifeCycleSvc:    orderLifeCycleSvc,
		notificationsService: notificationService,
		redis:                redis,
		inLockedTx: func(ctx context.Context, key string, fn func(txCtx context.Context) error) error {
			return dbutils.RunInTxLocked(ctx, db, key, fn)
		},
		fallbackProduct: func(ctx context.Context, merchantID string) (string, error) {
			return marketplacefallback.Product(ctx, db, merchantID, models.MarketplaceCategoryUberEats, marketplacefallback.UberEatsName)
		},
	}
}

func (s *Service) ProcessEvent(ctx context.Context, event modelsUber.UberWebhookEvent) error {
	log := logger.FromContext(ctx)

	// Idempotence Redis : l'événement est réservé le temps du traitement et marqué
	// « traité » seulement en cas de succès. Un échec lève la réservation, pour que
	// la nouvelle tentative d'Uber (même event_id) soit réellement traitée — marquer
	// l'événement avant le traitement faisait perdre la commande.
	var store idempotency.Store
	if s.redis != nil {
		store = s.redis
	}
	eventKey := helpers.GetWebhookUberEventKey(event.EventID)
	skipped, err := idempotency.Run(ctx, store, log, eventKey, models.WebhookUberEatsEventTTL, func() error {
		return s.dispatchEvent(ctx, event)
	})
	if skipped {
		log.Warn("[UBER EATS] Event already processed or in progress (Redis), skipping:" + eventKey)
	}
	return err
}

func (s *Service) dispatchEvent(ctx context.Context, event modelsUber.UberWebhookEvent) error {
	log := logger.FromContext(ctx)

	switch event.EventType {
	case "orders.notification":
		log.Info("New Order received for store " + event.Meta.ResourceID + " - " + event.Meta.UserID)
		return s.handleOrderNotification(ctx, event)

	case "orders.cancel":
		log.Info("Order canceled for store " + event.Meta.ResourceID + " - " + event.Meta.UserID)
		return s.HandleOrderCanceled(ctx, event.Meta.ResourceID)

	case "store.status.changed":
		log.Info("🏪 Store status changed for store " + event.Meta.ResourceID + " - " + event.Meta.UserID + " - New status: " + event.Meta.Status + " 🏪")
		// return s.HandleStoreStatusChanged(ctx, event.Meta.ResourceID, event.Meta.Status)
		return nil

	case "delivery.state_changed", "delivery.status":
		log.Info("Delivery status update for store " + event.Meta.ResourceID + " - " + event.Meta.UserID + " - New status: " + event.Meta.Status)
		return s.HandleDeliveryStatus(ctx, event.Meta.OrderID, event.Meta.Status)

	default:
		log.Error("[UBER EATS] Unhandled event type: " + event.EventType)
		return nil
	}
}

// VerifySignature contrôle la signature d'un webhook Uber Eats en mode
// observation (« soft run ») : le résultat est journalisé — erreur si la
// signature manque ou ne concorde pas, information si elle est vérifiée — et
// le webhook est TOUJOURS traité. Le rejet (401) ne sera activé qu'après une
// période sans aucune erreur en production.
func (s *Service) VerifySignature(ctx context.Context, headers http.Header, body []byte) {
	log := logger.FromContext(ctx)
	sig := headers.Get("X-Uber-Signature")
	fields := []zap.Field{zap.Int("body_bytes", len(body)), zap.Bool("signature_present", sig != "")}
	switch {
	case s.signatureSecret == "":
		log.Error("[UBER EATS] webhook signature not checked: UBER_EATS_CLIENT_SECRET is empty (soft run, webhook processed)", fields...)
	case sig == "":
		log.Error("[UBER EATS] webhook signature missing: no X-Uber-Signature header (soft run, webhook processed)", fields...)
	case !ueClient.VerifySignature(body, sig, s.signatureSecret):
		log.Error("[UBER EATS] webhook signature mismatch (soft run, webhook processed)", fields...)
	default:
		log.Info("[UBER EATS] webhook signature verified", fields...)
	}
}
