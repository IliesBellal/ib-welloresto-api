package service

import (
	"context"
	"time"

	"welloresto-api/internal/models"
	ordersModels "welloresto-api/internal/models"
	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

func (s *Service) mapUberItemsToOrderProducts(
	ctx context.Context,
	merchantID string,
	items []ueModels.UberCartItem,
) ([]ordersModels.OrderProductPayload, error) {

	var result []ordersModels.OrderProductPayload

	for _, item := range items {

		productID, err := s.productMappingRepo.FindProductIDByUberItemID(ctx, merchantID, item.ID)
		if err != nil {
			return nil, err
		}

		// Produit inconnu → création auto
		if productID == nil {
			newID, err := s.menuService.CreateProductFromExternal(
				ctx,
				merchantID,
				item.Title,
				"UBER IMPORT",
				item.Price.UnitPrice.Amount,
			)
			if err != nil {
				return nil, err
			}

			err = s.productMappingRepo.CreateProductMapping(ctx, merchantID, *newID, item.ID)
			if err != nil {
				return nil, err
			}
			productID = newID
		}

		payload := ordersModels.OrderProductPayload{
			ProductID:   *productID,
			ProductName: item.Title,
			Quantity:    item.Quantity,
			Price:       item.Price.UnitPrice.Amount,
			OrderedDate: time.Now().Format(time.RFC3339),
		}

		// Construire le commentaire en combinant instructions spéciales et allergies
		var commentContent *string
		if item.CustomerRequest != nil {
			var fullComment string

			// Ajouter les instructions d'allergie si elles existent
			if item.CustomerRequest.Allergy != nil && item.CustomerRequest.Allergy.Instructions != nil {
				fullComment += *item.CustomerRequest.Allergy.Instructions
			}

			// Ajouter les instructions spéciales si elles existent
			if item.CustomerRequest.SpecialInstructions != nil {
				if fullComment != "" {
					fullComment += " | "
				}
				fullComment += *item.CustomerRequest.SpecialInstructions
			}

			if fullComment != "" {
				commentContent = &fullComment
			}
		}

		if commentContent != nil {
			payload.Comment = &ordersModels.OrderItemCommentPayload{
				Content: *commentContent,
				UserID:  models.UberEatsWebhookUserID,
			}
		}

		config, err := s.mapModifiers(ctx, merchantID, item) // <-- Passage de la transaction
		if err != nil {
			return nil, err
		}
		payload.Config = config

		result = append(result, payload)
	}

	return result, nil
}
