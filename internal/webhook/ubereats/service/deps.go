package service

import (
	"context"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/googlemaps"
	"welloresto-api/internal/modules/ubereats"
)

type OrdersService interface {
	CreateOrder(ctx context.Context, token string, req *models.RequestObject) (*models.CreateOrderResult, error)
}

type StoreRepository interface {
	GetByStoreID(ctx context.Context, storeID string) (*ubereats.Store, error)
}

type CustomersService interface {
	GetByBrandCustomerID(ctx context.Context, brand string, brandCustomerID string) (*Customer, error)
	CreateOrUpdateFromExternal(ctx context.Context, input ExternalCustomerInput) (*Customer, error)
}

type UberEatsClient interface {
	GetOrderByURL(url string, bearer string, dest interface{}) error
}

type GoogleClient interface {
	GetAddressFromPlaceID(placeID string) (googlemaps.PlaceDetailsResponse, error)
}

type Customer struct {
	ID string
}

type ExternalCustomerInput struct {
	MerchantID string
	Name       string
	Phone      *string
	Address    *string
	Lat        *float64
	Lng        *float64
	Brand      string
	BrandID    string
}
