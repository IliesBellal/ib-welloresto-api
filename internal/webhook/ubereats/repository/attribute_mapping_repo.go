package repository

import (
	"context"
	"database/sql"
)

type AttributeMappingRepository struct {
	db *sql.DB
}

func (r *AttributeMappingRepository) CreateAttributeFromUberGroup(ctx context.Context, merchantID, name string) (string, error) {
	//TODO implement me
	panic("implement me")
}

func (r *AttributeMappingRepository) CreateOptionFromUber(ctx context.Context, attributeID, title string, price int) (string, error) {
	//TODO implement me
	panic("implement me")
}

func NewAttributeMappingRepository(db *sql.DB) *AttributeMappingRepository {
	return &AttributeMappingRepository{db: db}
}

// ---- Attributes ----
func (r *AttributeMappingRepository) GetAttributeIDByModifierGroupID(ctx context.Context, merchantID, groupID string) (*string, error) {
	var id string
	err := r.db.QueryRowContext(ctx,
		`SELECT configurable_attribute_id
		 FROM integration_uber_eats_attributes_mapping
		 WHERE merchant_id = ? AND modifier_group_id = ?`,
		merchantID, groupID).Scan(&id)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &id, nil
}

func (r *AttributeMappingRepository) CreateAttributeMapping(ctx context.Context, merchantID, attrID, groupID string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO integration_uber_eats_attributes_mapping(merchant_id, configurable_attribute_id, modifier_group_id)
		 VALUES(?, ?, ?)`,
		merchantID, attrID, groupID)
	return err
}

// ---- Options ----
func (r *AttributeMappingRepository) GetOptionIDByUberItemID(ctx context.Context, attributeID, uberItemID string) (*string, error) {
	var id string
	err := r.db.QueryRowContext(ctx,
		`SELECT configurable_attribute_option_id
		 FROM integration_uber_eats_options_mapping
		 WHERE configurable_attribute_id = ? AND item_id = ?`,
		attributeID, uberItemID).Scan(&id)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &id, nil
}

func (r *AttributeMappingRepository) CreateOptionMapping(ctx context.Context, merchantID, optionID, uberItemID string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO integration_uber_eats_options_mapping(merchant_id, configurable_attribute_option_id, item_id)
		 VALUES(?, ?, ?)`,
		merchantID, optionID, uberItemID)
	return err
}
