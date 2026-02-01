package service

import (
	"context"
	"encoding/json"
	"log"

	ueModels "welloresto-api/internal/modules/webhook/ubereats/models"
)

func (s *Service) handleOrderNotification(ctx context.Context, event ueModels.WebhookEvent) error {
	var payload struct {
		ResourceHref string `json:"resource_href"`
		StoreID      string `json:"store_id"`
	}

	if err := json.Unmarshal(event.Resource, &payload); err != nil {
		return err
	}

	store, err := s.storeRepo.GetByStoreID(ctx, payload.StoreID)
	if err != nil {
		return err
	}

	var order ueModels.UberOrder
	err = s.uberClient.GetOrderByURL(payload.ResourceHref, store.BearerToken, &order)
	if err != nil {
		return err
	}

	customer, _ := s.customersService.CreateOrUpdateFromExternal(ctx, ExternalCustomerInput{
		MerchantID: store.MerchantID,
		Name:       order.Eater.FirstName + " " + order.Eater.LastName,
		Phone:      &order.Eater.Phone,
		Brand:      "UBER_EATS",
		BrandID:    order.ID,
	})

	products, err := s.mapUberItemsToOrderProducts(ctx, store.MerchantID, order.Cart.Items)
	if err != nil {
		return err
	}

	req := MapUberOrderToRequest(&order, store.MerchantID, &customer.ID)
	req.Order.Products = products

	_, err = s.ordersService.PrepareCreateOrder(ctx, s.systemToken, req)
	if err != nil {
		return err
	}

	log.Println("[UBER EATS] Order imported:", order.ID)
	return nil
}
