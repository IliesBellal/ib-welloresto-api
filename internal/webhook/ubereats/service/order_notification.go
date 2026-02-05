package service

import (
	"context"
	"encoding/json"
	"log"

	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

func (s *Service) handleOrderNotification(ctx context.Context, event ueModels.WebhookEvent) error {
	var payload struct {
		ResourceHref string `json:"resource_href"`
		StoreID      string `json:"store_id"`
	}

	if err := json.Unmarshal(event.Resource, &payload); err != nil {
		return err
	}

	store, err := s.uberClient.GetByStoreID(ctx, payload.StoreID)
	if err != nil {
		return err
	}

	var order ueModels.UberOrder
	order, err = s.uberClient.GetOrderByURL(ctx, payload.ResourceHref, store.BearerToken)
	if err != nil {
		return err
	}

	products, err := s.mapUberItemsToOrderProducts(ctx, store.MerchantID, order.Cart.Items)
	if err != nil {
		return err
	}

	req := MapUberOrderToRequest(&order, store.MerchantID)
	req.Order.Products = products

	_, err = s.ordersService.PrepareCreateOrder(ctx, s.systemToken, req)
	if err != nil {
		return err
	}

	log.Println("[UBER EATS] Order imported:", order.ID)
	return nil
}
