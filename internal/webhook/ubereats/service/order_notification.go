package service

import (
	"context"
	"log"

	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

func (s *Service) handleOrderNotification(ctx context.Context, event ueModels.UberWebhookEvent) error {

	store, err := s.uberClient.GetByStoreID(ctx, event.Meta.UserID)
	if err != nil {
		return err
	}

	var order *ueModels.UberOrder
	order, err = s.uberClient.GetOrderByURL(ctx, event.ResourceHref)
	if err != nil {
		return err
	}

	products, err := s.mapUberItemsToOrderProducts(ctx, store.MerchantID, order.Cart.Items)
	if err != nil {
		return err
	}

	req := MapUberOrderToRequest(order, store.MerchantID)
	req.Order.Products = products

	_, err = s.ordersService.PrepareCreateOrder(ctx, s.systemToken, req)
	if err != nil {
		return err
	}

	log.Println("[UBER EATS] Order imported:", order.ID)
	return nil
}
