package service

import (
	"context"
	"time"

	ordersModels "welloresto-api/internal/models"
	ueModels "welloresto-api/internal/modules/webhook/ubereats/models"
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
			newID, err := s.catalogService.CreateProductFromExternal(
				ctx,
				merchantID,
				item.Title,
				"UBER IMPORT",
				item.Price.UnitPrice.Amount,
			)
			if err != nil {
				return nil, err
			}

			err = s.productMappingRepo.CreateProductMapping(ctx, merchantID, newID, item.ID)
			if err != nil {
				return nil, err
			}
			productID = &newID
		}

		payload := ordersModels.OrderProductPayload{
			ProductID:   *productID,
			ProductName: item.Title,
			Quantity:    item.Quantity,
			Price:       item.Price.UnitPrice.Amount,
			OrderedDate: time.Now().Format(time.RFC3339),
		}

		config, err := s.mapModifiers(ctx, merchantID, item)
		if err != nil {
			return nil, err
		}
		payload.Config = config

		result = append(result, payload)
	}

	return result, nil
}
