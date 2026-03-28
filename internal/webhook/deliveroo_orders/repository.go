package deliveroo_orders

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/utils/dbutils"
)

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// MerchantData contient les infos récupérées via le LocationID
type MerchantData struct {
	MerchantID       string
	AutoAcceptOrders bool
}

// GetMerchantByLocationID trouve le marchand lié à l'ID Deliveroo
func (r *Repository) GetMerchantByLocationID(ctx context.Context, locationID string) (*MerchantData, error) {
	if locationID == "" {
		return nil, errors.New("location_id is empty")
	}

	query := `
		SELECT id.merchant_id, id.auto_accept_orders
		FROM merchant m
		INNER JOIN integration_deliveroo id on id.merchant_id = m.id
		WHERE id.location_id = ?`

	var data MerchantData
	var autoAccept int // Souvent stocké en int/tinyint en SQL
	err := r.database.QueryRowContext(ctx, query, locationID).Scan(&data.MerchantID, &autoAccept)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no merchant found for location_id %s", locationID)
		}
		return nil, err
	}
	data.AutoAcceptOrders = autoAccept == 1
	return &data, nil
}

// GetNextOrderNum réplique la logique du switch case 99 -> 1 du PHP
func (r *Repository) GetNextOrderNum(ctx context.Context, merchantID string) (string, error) {
	query := `
		SELECT order_num
		FROM orders
		WHERE merchant_id = ?
		ORDER BY order_id DESC
		LIMIT 1`

	var lastOrderNumStr string
	err := r.database.QueryRowContext(ctx, query, merchantID).Scan(&lastOrderNumStr)
	if err != nil && err != sql.ErrNoRows {
		return "1", err
	}

	if err == sql.ErrNoRows {
		return "1", nil
	}

	lastNum, _ := strconv.Atoi(lastOrderNumStr)
	if lastNum >= 99 {
		return "1", nil
	}
	return strconv.Itoa(lastNum + 1), nil
}

// SyncOption gère la logique complexe des attributs et options (sync ou création)
// Retourne (ConfigurableAttributeID, ConfigurableAttributeOptionID)
func (r *Repository) SyncOption(ctx context.Context, merchantID string, productID string, mod DeliverooModifier) (string, string, error) {
	db := dbutils.GetDB(ctx, r.database)
	// 1. Essayer de trouver le mapping
	query := `
		SELECT opt.id AS option_id, opt.configurable_attribute_id AS attribute_id
		FROM configurable_attribute_options opt
		JOIN integration_deliveroo_options_mapping map ON opt.id = map.configurable_attribute_option_id
		WHERE map.item_id = ? AND map.merchant_id = ?`

	var optionID, attributeID string
	err := db.QueryRowContext(ctx, query, mod.PosItemID, merchantID).Scan(&optionID, &attributeID)

	if err == nil {
		// Trouvé, mais il faut s'assurer que le produit est lié à l'attribut (logique du PHP step 2)
		if errLink := r.ensureProductAttributeLink(ctx, productID, attributeID); errLink != nil {
			return "", "", errLink
		}
		return attributeID, optionID, nil
	}

	// a. Get or Create Default Group
	attributeID, err = r.getOrCreateDefaultGroupTx(ctx, merchantID)
	if err != nil {
		return "", "", err
	}

	// b. Create Option
	resOpt, err := db.ExecContext(ctx, `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, extra_price)
		VALUES (?, ?, ?)`,
		attributeID, mod.Name, mod.UnitPrice.Fractional)
	if err != nil {
		return "", "", err
	}
	optIDInt, _ := resOpt.LastInsertId()
	optionID = strconv.FormatInt(optIDInt, 10)

	// c. Create Mapping
	_, err = db.ExecContext(ctx, `
		INSERT INTO integration_deliveroo_options_mapping (merchant_id, configurable_attribute_option_id, item_id)
		VALUES (?, ?, ?)`,
		merchantID, optionID, mod.PosItemID)
	if err != nil {
		return "", "", err
	}

	// d. Link Product to Attribute (si pas fait)
	// Note: On le fait dans la transaction ici pour être sûr
	var count int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM product_configurable_attribute WHERE product_id = ? AND configurable_attribute_id = ?", productID, attributeID).Scan(&count)
	if count == 0 {
		_, err = db.ExecContext(ctx, "INSERT INTO product_configurable_attribute (product_id, configurable_attribute_id) VALUES (?, ?)", productID, attributeID)
		if err != nil {
			return "", "", err
		}
	}

	return attributeID, optionID, nil
}

func (r *Repository) ensureProductAttributeLink(ctx context.Context, productID, attributeID string) error {
	var count int
	err := r.database.QueryRowContext(ctx, "SELECT COUNT(*) FROM product_configurable_attribute WHERE product_id = ? AND configurable_attribute_id = ?", productID, attributeID).Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		_, err := r.database.ExecContext(ctx, "INSERT INTO product_configurable_attribute (product_id, configurable_attribute_id) VALUES (?, ?)", productID, attributeID)
		return err
	}
	return nil
}

func (r *Repository) getOrCreateDefaultGroupTx(ctx context.Context, merchantID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)

	name := "Deliveroo Options"
	var id int
	err := db.QueryRowContext(ctx, "SELECT id FROM configurable_attributes WHERE merchant_id = ? AND name = ?", merchantID, name).Scan(&id)
	if err == nil {
		return strconv.Itoa(id), nil
	}

	res, err := db.ExecContext(ctx, `
		INSERT INTO configurable_attributes (merchant_id, brand, name, title, is_required, min_options, max_options)
		VALUES (?, 'DELIVEROO', ?, 'Options Deliveroo', 0, 0, 99)`,
		merchantID, name)

	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
		return "", err
	}

	newID, _ := res.LastInsertId()
	return strconv.FormatInt(newID, 10), nil
}

// SyncProduct : Logique de synchronisation/création de produit
func (r *Repository) SyncProduct(ctx context.Context, merchantID string, item DeliverooItem) (string, error) {
	db := dbutils.GetDB(ctx, r.database)

	// (Code identique à la version précédente - simplifié ici pour la lecture mais à inclure complet)
	// 1. Check Mapping
	queryMap := `
		SELECT p.product_id
		FROM products p
		INNER JOIN integration_deliveroo_products_mapping map on p.product_id = map.product_id
		WHERE map.item_id = ? AND map.merchant_id = ? AND map.enabled = '1' AND p.enabled = '1'`
	var productID string
	err := db.QueryRowContext(ctx, queryMap, item.PosItemID, merchantID).Scan(&productID)
	if err == nil {
		return productID, nil
	}

	res, err := db.ExecContext(ctx, `INSERT INTO products (merchant_id, name, product_desc, price) VALUES(?, ?, ?, ?)`, merchantID, item.Name, item.OperationalName, item.UnitPrice.Fractional)
	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
		return "", err
	}
	newID, _ := res.LastInsertId()
	productID = strconv.FormatInt(newID, 10)

	_, err = db.ExecContext(ctx, `INSERT INTO integration_deliveroo_products_mapping(merchant_id, product_id, item_id, item_name) VALUES(?, ?, ?, ?)`, merchantID, productID, item.PosItemID, item.OperationalName)
	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
		return "", err
	}

	return productID, nil
}

// --- Status Update Logic (Transaction Based) ---

// UpdateOrderRejected met à jour la commande en REJECTED/CANCELED
func (r *Repository) UpdateOrderRejected(ctx context.Context, brandOrderID string, status string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `
		UPDATE orders
		SET brand_status = ?, state = 'CLOSED', merchant_approval = 'DENIED'
		WHERE brand_order_id = ?`
	_, err := db.ExecContext(ctx, query, status, brandOrderID)
	return err
}

// DisablePayments désactive les paiements pour une commande annulée
func (r *Repository) DisablePayments(ctx context.Context, brandOrderID string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `
		UPDATE payments p
		JOIN orders o ON p.order_id = o.order_id
		SET p.enabled = FALSE
		WHERE o.brand_order_id = ?`
	_, err := db.ExecContext(ctx, query, brandOrderID)

	return err
}

// UpdateOrderAccepted met à jour le statut ACCEPTED (avec logique toggle scheduled du PHP)
func (r *Repository) UpdateOrderAccepted(ctx context.Context, brandOrderID string, isScheduledToggle bool) error {
	db := dbutils.GetDB(ctx, r.database)

	var query string
	if isScheduledToggle {
		// Logique PHP: case WHEN brand_status = 'scheduled' then 'accepted' else 'scheduled' end
		query = `
			UPDATE orders
			SET brand_status = CASE WHEN brand_status = 'scheduled' THEN 'accepted' ELSE 'scheduled' END,
			    merchant_approval = 'ACCEPTED'
			WHERE brand_order_id = ?`
	} else {
		query = `
			UPDATE orders
			SET brand_status = 'accepted', merchant_approval = 'ACCEPTED'
			WHERE brand_order_id = ?`
	}
	_, err := db.ExecContext(ctx, query, brandOrderID)

	return err
}

// UpdateOrderConfirmed met à jour le statut CONFIRMED
func (r *Repository) UpdateOrderConfirmed(ctx context.Context, brandOrderID string) error {
	db := dbutils.GetDB(ctx, r.database)

	query := `
		UPDATE orders
		SET brand_status = 'confirmed', merchant_approval = 'ACCEPTED'
		WHERE brand_order_id = ?`
	_, err := db.ExecContext(ctx, query, brandOrderID)
	return err
}

// GetOrderIDByBrandID récupère l'ID interne (order_id) via l'ID Deliveroo
func (r *Repository) GetOrderIDByBrandIDTx(ctx context.Context, brandOrderID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `SELECT order_id FROM orders WHERE brand_order_id = ?`
	var orderID string
	err := db.QueryRowContext(ctx, query, brandOrderID).Scan(&orderID)
	if err != nil {
		return "", err
	}
	return orderID, nil
}

func (r *Repository) GetOrderIDByBrandID(ctx context.Context, brandOrderID string) (string, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `SELECT order_id FROM orders WHERE brand_order_id = ?`

	var orderID string
	err := db.QueryRowContext(ctx, query, brandOrderID).Scan(&orderID)
	if err != nil {
		return "", err
	}

	return orderID, nil
}

// GetProductMapping récupère le mapping d'un produit Deliveroo pour un marchand donné
func (r *Repository) GetProductMapping(ctx context.Context, merchantID string, deliverooItemID string) (*DeliverooProductMapping, error) {
	db := dbutils.GetDB(ctx, r.database)

	// La requête SQL issue de ton code PHP
	// J'utilise ? comme placeholder (standard MySQL/MariaDB).
	// Si tu es sur PostgreSQL, remplace par $1, $2
	query := `
        SELECT map.item_name, map.item_id
        FROM integration_deliveroo_products_mapping map
        INNER JOIN products p ON p.product_id = map.product_id
        WHERE map.item_id = ?
        AND map.merchant_id = ?
    `

	var mapping DeliverooProductMapping

	// Exécution de la requête
	err := db.QueryRowContext(ctx, query, deliverooItemID, merchantID).Scan(
		&mapping.ItemName,
		&mapping.ItemID,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			// Aucun mapping trouvé => C'est le cas "pos_item_id_not_found" du PHP
			return nil, nil
		}
		// Erreur technique (DB down, syntaxe, etc.)
		return nil, fmt.Errorf("error fetching product mapping: %w", err)
	}

	return &mapping, nil
}
