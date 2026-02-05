package service

import (
	"context"
	"database/sql"
	"net/http"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/modules/googlemaps"
	"welloresto-api/internal/modules/menu"
	"welloresto-api/internal/modules/notification"

	"welloresto-api/internal/modules/order_life_cycle"
	"welloresto-api/internal/modules/orders"
	"welloresto-api/internal/modules/ubereats"
	ueClient "welloresto-api/internal/webhook/ubereats/client"
	"welloresto-api/internal/webhook/ubereats/models"
	"welloresto-api/internal/webhook/ubereats/repository"
)

type Service struct {
	systemToken     string
	signatureSecret string

	productMappingRepo   ProductMappingRepository
	attributeMappingRepo AttributeMappingRepository

	googleClient         *googlemaps.GoogleMapsClient
	uberClient           *ubereats.UberEatsService
	ordersService        *orders.OrdersService
	menuService          *menu.MenuService
	ordersRepo           *repository.OrdersRepository
	orderLifeCycleSvc    *order_life_cycle.OrdersLifeCycleService
	notificationsService *notification.NotificationService
	db                   *sql.DB
}

type ProductMappingRepository interface {
	FindProductIDByUberItemID(ctx context.Context, merchantID, uberItemID string) (*string, error)
	CreateProductMapping(ctx context.Context, merchantID, productID, uberItemID string) error
}

type CatalogService interface {
	CreateProductFromExternal(ctx context.Context, merchantID, name, desc string, price int) (string, error)
}

type AttributeMappingRepository interface {
	GetAttributeIDByModifierGroupID(ctx context.Context, merchantID, groupID string) (*string, error)
	CreateAttributeFromUberGroup(ctx context.Context, merchantID, name string) (string, error)
	CreateAttributeMapping(ctx context.Context, merchantID, attrID, groupID string) error

	GetOptionIDByUberItemID(ctx context.Context, attributeID, uberItemID string) (*string, error)
	CreateOptionFromUber(ctx context.Context, attributeID, title string, price int) (string, error)
	CreateOptionMapping(ctx context.Context, merchantID, optionID, uberItemID string) error
}

type OrderLifeCycleService interface {
	SendUpdateOrderNotification(merchantID, orderID string)
	SetOrderAccepted(source, merchantID, orderID string)
	SetDelivered(source string, auto bool, merchantID, orderID string)
}

func NewService(
	db *sql.DB,
	signatureSecret string,
	systemToken string,
	uberClient *ubereats.UberEatsService,
	googleClient *googlemaps.GoogleMapsClient,
	ordersService *orders.OrdersService,
	menuService *menu.MenuService,
	orderLifeCycleSvc *order_life_cycle.OrdersLifeCycleService,
	notificationService *notification.NotificationService,
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
		systemToken:          systemToken,
	}
}

func (s *Service) ProcessEvent(ctx context.Context, event models.WebhookEvent) error {

	log := logger.FromContext(ctx)

	switch event.EventType {

	case "orders.notification":
		return s.handleOrderNotification(ctx, event)

	case "orders.cancel":
		return s.HandleOrderCanceled(ctx, event.Meta.ResourceID)

	case "delivery.status":
		return s.HandleDeliveryStatus(ctx, event.Meta.OrderID, event.Meta.Status)

	default:
		log.Error("[UBER EATS] Unhandled event type: %s" + event.EventType)
		return nil
	}
}

func (s *Service) VerifySignature(ctx context.Context, headers http.Header, body []byte) {
	sig := headers.Get("X-Uber-Signature")
	ok := ueClient.VerifySignature(body, sig, s.signatureSecret)
	log := logger.FromContext(ctx)

	if !ok {
		log.Error("[UBER EATS] Invalid signature")
	}
}
