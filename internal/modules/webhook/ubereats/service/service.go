package service

import (
	"context"
	"database/sql"
	"net/http"
	"welloresto-api/internal/logger"

	"welloresto-api/internal/modules/order_life_cycle"
	"welloresto-api/internal/modules/orders"
	ueClient "welloresto-api/internal/modules/webhook/ubereats/client"
	"welloresto-api/internal/modules/webhook/ubereats/models"
	"welloresto-api/internal/modules/webhook/ubereats/repository"
)

type Service struct {
	storeRepo            StoreRepository
	uberClient           UberEatsClient
	googleClient         GoogleClient
	ordersService        orders.OrdersService
	customersService     CustomersService
	systemToken          string
	productMappingRepo   ProductMappingRepository
	attributeMappingRepo AttributeMappingRepository
	catalogService       CatalogService
	ordersRepo           *repository.OrdersRepository
	orderLifeCycleSvc    order_life_cycle.OrdersLifeCycleService
	db                   *sql.DB
	signatureSecret      string
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
	storeRepo StoreRepository,
	uberClient UberEatsClient,
	googleClient GoogleClient,
	ordersService orders.OrdersService,
	customersService CustomersService,
	productMappingRepo ProductMappingRepository,
	attributeMappingRepo AttributeMappingRepository,
	catalogService CatalogService,
	ordersRepo *repository.OrdersRepository,
	orderLifeCycleSvc order_life_cycle.OrdersLifeCycleService,
	systemToken string,
) *Service {
	return &Service{
		db:                   db,
		signatureSecret:      signatureSecret,
		storeRepo:            storeRepo,
		uberClient:           uberClient,
		googleClient:         googleClient,
		ordersService:        ordersService,
		customersService:     customersService,
		productMappingRepo:   productMappingRepo,
		attributeMappingRepo: attributeMappingRepo,
		catalogService:       catalogService,
		ordersRepo:           ordersRepo,
		orderLifeCycleSvc:    orderLifeCycleSvc,
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
