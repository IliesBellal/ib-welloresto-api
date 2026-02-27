package service

import (
	"context"
	"database/sql"

	ordersModels "welloresto-api/internal/models"
	ueModels "welloresto-api/internal/webhook/ubereats/models"
)

func (s *Service) mapModifiers(
	ctx context.Context,
	tx *sql.Tx,
	merchantID string,
	item ueModels.UberCartItem,
) (*ordersModels.ProductConfiguration, error) {

	if len(item.SelectedModifierGroups) == 0 {
		return nil, nil
	}

	var attrs []ordersModels.ConfigurationAttribute

	for _, group := range item.SelectedModifierGroups {

		attrID, err := s.attributeMappingRepo.GetAttributeIDByModifierGroupID(ctx, tx, merchantID, group.ID)
		if err != nil {
			return nil, err
		}

		if attrID == nil {
			newAttrID, err := s.attributeMappingRepo.CreateAttributeFromUberGroup(ctx, tx, merchantID, group.Title)
			if err != nil {
				return nil, err
			}
			err = s.attributeMappingRepo.CreateAttributeMapping(ctx, tx, merchantID, newAttrID, group.ID)
			if err != nil {
				return nil, err
			}
			attrID = &newAttrID
		}

		var options []ordersModels.ConfigurationOption

		for _, opt := range group.SelectedItems {
			optID, err := s.attributeMappingRepo.GetOptionIDByUberItemID(ctx, tx, *attrID, opt.ID)
			if err != nil {
				return nil, err
			}

			if optID == nil {
				newOptID, err := s.attributeMappingRepo.CreateOptionFromUber(ctx, tx, *attrID, opt.Title, opt.Price.UnitPrice.Amount)
				if err != nil {
					return nil, err
				}
				err = s.attributeMappingRepo.CreateOptionMapping(ctx, tx, merchantID, newOptID, opt.ID)
				if err != nil {
					return nil, err
				}
				optID = &newOptID
			}

			options = append(options, ordersModels.ConfigurationOption{
				ID:       *optID,
				Quantity: opt.Quantity,
			})
		}

		attrs = append(attrs, ordersModels.ConfigurationAttribute{
			ID:      *attrID,
			Options: options,
		})
	}

	return &ordersModels.ProductConfiguration{Attributes: attrs}, nil
}
