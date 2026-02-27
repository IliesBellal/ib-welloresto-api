package service

import (
	"context"
	"log"

	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

func (s *Service) handleOrderNotification(ctx context.Context, event ueModels.UberWebhookEvent) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // Rollback automatique si pas de commit

	store, err := s.uberClient.GetByStoreID(tx, event.Meta.UserID)
	if err != nil {
		return err
	}

	var order *ueModels.UberOrder
	order, err = s.uberClient.GetOrderByURL(tx, event.ResourceHref)
	if err != nil {
		return err
	}

	products, err := s.mapUberItemsToOrderProducts(ctx, store.MerchantID, order.Cart.Items)
	if err != nil {
		return err
	}

	req := MapUberOrderToRequest(order, store.MerchantID)
	req.Order.Products = products

	req.MerchantID = store.MerchantID
	createdBy := "WEBHOOK_UBER_EATS"
	req.Order.CreatedBy = &createdBy

	err = tx.Commit()
	if err != nil {
		return err
	}

	_, err = s.ordersService.CreateOrder(context.Background(), req)
	if err != nil {
		return err
	}

	log.Println("[UBER EATS] Order imported:", order.ID)
	return nil
}
