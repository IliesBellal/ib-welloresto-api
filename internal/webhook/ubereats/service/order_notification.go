package service

import (
	"context"
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

	// 2. Début de la logique normale
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	store, err := s.uberClient.GetByStoreID(tx, event.Meta.UserID)
	if err != nil {
		return err
	}

	// On récupère la commande chez Uber
	order, err := s.uberClient.GetOrderByURL(tx, event.ResourceHref)
	if err != nil {
		return err
	}

	// 3. Mapping et création
	products, err := s.mapUberItemsToOrderProducts(ctx, tx, store.MerchantID, order.Cart.Items)
	if err != nil {
		return err
	}

	req := MapUberOrderToRequest(order, store.MerchantID)
	req.Order.Products = products
	req.MerchantID = store.MerchantID
	createdBy := models.UberEatsWebhookUserID
	req.Order.CreatedBy = &createdBy

	// On valide la transaction locale
	if err = tx.Commit(); err != nil {
		return err
	}

	// 4. Création finale de la commande
	_, err = s.ordersService.CreateOrder(context.Background(), req)
	if err != nil {
		// Si l'erreur est une violation d'index UNIQUE (doublon), on log et on ignore
		if strings.Contains(err.Error(), "Duplicate entry") {
			log.Warn("[UBER EATS] Race condition detected: Order already created by another thread")
			return nil
		}
		return err
	}

	log.Info("[UBER EATS] Order imported:" + order.ID)
	return nil
}
