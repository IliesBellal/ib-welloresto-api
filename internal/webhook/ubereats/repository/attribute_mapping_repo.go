package repository

import (
	"context"
	"database/sql"
	"regexp"
	"strconv"
	"strings"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/utils/dbutils"
)

type AttributeMappingRepository struct {
	database *sql.DB
}

func NewAttributeMappingRepository(db *sql.DB) *AttributeMappingRepository {
	return &AttributeMappingRepository{database: db}
}

func (r *AttributeMappingRepository) CreateAttributeFromUberGroup(ctx context.Context, merchantID, title string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Équivalent PHP: preg_replace('/[^a-zA-Z0-9]/', '_', title)
	reg := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	cleanTitle := reg.ReplaceAllString(title, "_")

	// Équivalent PHP: 'ue_' . strtolower(...)
	attrName := "ue_" + strings.ToLower(cleanTitle)

	res, err := db.ExecContext(ctx, `
		INSERT INTO configurable_attributes (merchant_id, brand, name, title, min_options, max_options, is_required)
		VALUES (?, 'UBER_EATS', ?, ?, 0, 99, 0)
	`, merchantID, attrName, title)

	if err != nil {
		log.Error("Error creating attribute from Uber group: " + err.Error())
		return "", err
	}

	// Récupération de l'ID généré (équivalent de lastInsertId())
	id, err := res.LastInsertId()
	if err != nil {
		log.Error("Error getting last insert ID for attribute: " + err.Error())
		return "", err
	}

	return strconv.FormatInt(id, 10), nil
}

func (r *AttributeMappingRepository) CreateOptionFromUber(ctx context.Context, attributeID, title string, price int) (string, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	res, err := db.ExecContext(ctx, `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, extra_price)
		VALUES (?, ?, ?)
	`, attributeID, title, price)

	if err != nil {
		log.Error("Error creating option from Uber item: " + err.Error())
		return "", err
	}

	id, err := res.LastInsertId()
	if err != nil {
		log.Error("Error getting last insert ID for option: " + err.Error())
		return "", err
	}

	return strconv.FormatInt(id, 10), nil
}

// ---- Attributes ----
func (r *AttributeMappingRepository) GetAttributeIDByModifierGroupID(ctx context.Context, merchantID, groupID string) (*string, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var id string
	err := db.QueryRowContext(ctx,
		`SELECT configurable_attribute_id
        FROM integration_uber_eats_attributes_mapping
        WHERE merchant_id = ? AND modifier_group_id = ?`,
		merchantID, groupID).Scan(&id)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		log.Error("Error fetching attribute ID by modifier group ID: " + err.Error())
		return nil, err
	}
	return &id, nil
}

func (r *AttributeMappingRepository) CreateAttributeMapping(ctx context.Context, merchantID, attrID, groupID string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	_, err := db.ExecContext(ctx,
		`INSERT INTO integration_uber_eats_attributes_mapping(merchant_id, configurable_attribute_id, modifier_group_id)
        VALUES(?, ?, ?)`,
		merchantID, attrID, groupID)
	if err != nil {
		log.Error("Error creating attribute mapping: " + err.Error())
	}
	return err
}

// ---- Options ----
func (r *AttributeMappingRepository) GetOptionIDByUberItemID(ctx context.Context, attributeID, uberItemID string) (*string, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var id string
	err := db.QueryRowContext(ctx,
		`SELECT cao.id
		FROM configurable_attribute_options cao
		JOIN integration_uber_eats_options_mapping map ON cao.id = map.configurable_attribute_option_id
        WHERE cao.configurable_attribute_id = ? AND map.item_id = ?`,
		attributeID, uberItemID).Scan(&id)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		log.Error("Error fetching option ID by Uber item ID: " + err.Error())
		return nil, err
	}
	return &id, nil
}

func (r *AttributeMappingRepository) CreateOptionMapping(ctx context.Context, merchantID, optionID, uberItemID string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	_, err := db.ExecContext(ctx,
		`INSERT INTO integration_uber_eats_options_mapping(merchant_id, configurable_attribute_option_id, item_id)
        VALUES(?, ?, ?)`,
		merchantID, optionID, uberItemID)

	if err != nil {
		log.Error("Error creating option mapping: " + err.Error())
	}

	return err
}
