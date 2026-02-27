package service

import (
	"context"
	"database/sql"
	"time"

	ordersModels "welloresto-api/internal/models"
	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

func (s *Service) mapUberItemsToOrderProducts(
	ctx context.Context,
	tx *sql.Tx, // <-- Ajout de la transaction
	merchantID string,
	items []ueModels.UberCartItem,
) ([]ordersModels.OrderProductPayload, error) {

	var result []ordersModels.OrderProductPayload

	for _, item := range items {

		productID, err := s.productMappingRepo.FindProductIDByUberItemID(ctx, tx, merchantID, item.ID)
		if err != nil {
			return nil, err
		}

		// Produit inconnu → création auto
		if productID == nil {
			newID, err := s.menuService.CreateProductFromExternal(
				ctx,
				tx,
				merchantID,
				item.Title,
				"UBER IMPORT",
				item.Price.UnitPrice.Amount,
			)
			if err != nil {
				return nil, err
			}

			err = s.productMappingRepo.CreateProductMapping(ctx, tx, merchantID, *newID, item.ID)
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

		config, err := s.mapModifiers(ctx, tx, merchantID, item) // <-- Passage de la transaction
		if err != nil {
			return nil, err
		}
		payload.Config = config

		result = append(result, payload)
	}

	return result, nil
}
