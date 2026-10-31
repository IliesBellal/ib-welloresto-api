package service

import (
	"context"
	"fmt"
	"strings"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

func (s *Service) handleOrderNotification(ctx context.Context, event ueModels.UberWebhookEvent) error {
	log := logger.FromContext(ctx)

	// 1. VÉRIFICATION IMMÉDIATE (Idempotence)
	// On vérifie en base si cet ID Uber a déjà été traité
	exists, err := s.ordersService.ExistsByBrandOrderID(ctx, models.BrandUberEats, event.Meta.ResourceID)
	if err != nil {
		return err
	}
	if exists {
		log.Info("[UBER EATS] Order already processed, skipping:" + event.Meta.ResourceID)
		return nil // On renvoie nil pour que le handler réponde 200 OK
	}

	store, err := s.uberClient.GetByStoreID(ctx, event.Meta.UserID)
	if err != nil {
		return err
	}

	// On récupère la commande chez Uber
	order, err := s.uberClient.GetOrderByURL(ctx, event.ResourceHref)
	if err != nil {
		return err
	}

	// 3. Mapping et création
	products, err := s.mapUberItemsToOrderProducts(ctx, store.MerchantID, order.Cart.Items)
	if err != nil {
		return err
	}

	req := MapUberOrderToRequest(ctx, order, store.MerchantID)
	req.Order.Products = applyPromotions(log, order, products)
	req.MerchantID = store.MerchantID
	req.Order.BrandStoreID = &store.StoreID
	createdBy := models.UberEatsWebhookUserID
	req.Order.CreatedBy = &createdBy

	// 4. Création finale de la commande. Acceptée automatiquement, elle n'est
	// notifiée qu'une fois l'acceptation tranchée (AutoAcceptOrder).
	createOrder := s.orderLifeCycleSvc.CreateOrder
	if store.AutoAcceptOrders {
		createOrder = s.orderLifeCycleSvc.CreateOrderWithoutNotification
	}
	result, err := createOrder(ctx, req)
	if err != nil {
		// Si l'erreur est une violation d'index UNIQUE (doublon), on log et on ignore
		if strings.Contains(err.Error(), "Duplicate entry") {
			log.Warn("[UBER EATS] Race condition detected: Order already created by another thread")
			return nil
		}
		return err
	}
	// Une création qui ne renvoie pas d'erreur mais pas non plus « success » n'a
	// rien écrit : sans ce contrôle, la commande disparaissait avec un « importée ».
	if result == nil || result.Status != "success" {
		status := ""
		if result != nil {
			status = result.Status
		}
		return fmt.Errorf("[UBER EATS] order %s not created (status %q)", order.ID, status)
	}

	if len(result.UnavailableProducts) > 0 {
		// Le client a payé : la commande est créée, mais le personnel doit décider
		// (la rupture est signalée en tête du commentaire et la commande notifiée).
		log.Warn("[UBER EATS] Order imported with out-of-stock products, not auto-accepted: " + order.ID)
	} else if store.AutoAcceptOrders {
		if err := s.orderLifeCycleSvc.AutoAcceptOrder(ctx, models.UberEatsWebhookUserID, store.MerchantID, result.OrderID); err != nil {
			log.Error("Error auto-accepting order " + result.OrderID + ": " + err.Error())
		}
	}

	log.Info("[UBER EATS] Order imported:" + order.ID)
	return nil
}
