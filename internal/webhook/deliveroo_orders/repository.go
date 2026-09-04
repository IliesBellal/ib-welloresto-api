package deliveroo_orders

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
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

	db := dbx.GetDB(ctx, r.database)

	// merchant.id is an integer identity while integration_deliveroo.merchant_id
	// is varchar (merchant_id is carried as a string everywhere else in the Go
	// code, see 12-merchant-id-unification.md) — MySQL implicitly casts across
	// the join, Postgres requires an explicit one, and CAST syntax itself
	// differs per dialect (CHAR vs TEXT).
	joinCast := "CAST(m.id AS CHAR)"
	if dbx.ActiveDialect() == dbx.Postgres {
		joinCast = "CAST(m.id AS TEXT)"
	}
	query := fmt.Sprintf(`
		SELECT id.merchant_id, id.auto_accept_orders
		FROM merchant m
		INNER JOIN integration_deliveroo id on id.merchant_id = %s
		WHERE id.location_id = ?`, joinCast)

	var data MerchantData
	err := db.QueryRowContext(ctx, query, locationID).Scan(&data.MerchantID, &data.AutoAcceptOrders)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no merchant found for location_id %s", locationID)
		}
		return nil, err
	}
	return &data, nil
}

// GetNextOrderNum réplique la logique du switch case 99 -> 1 du PHP
func (r *Repository) GetNextOrderNum(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `
		SELECT order_num
		FROM orders
		WHERE merchant_id = ?
		ORDER BY order_id DESC
		LIMIT 1`

	var lastOrderNumStr string
	err := db.QueryRowContext(ctx, query, merchantID).Scan(&lastOrderNumStr)
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
	db := dbx.GetDB(ctx, r.database)
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
	optIDInt, err := db.InsertReturningID(ctx, `
		INSERT INTO configurable_attribute_options (configurable_attribute_id, title, extra_price)
		VALUES (?, ?, ?)`,
		"id", attributeID, mod.Name, mod.UnitPrice.Fractional)
	if err != nil {
		return "", "", err
	}
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
	db := dbx.GetDB(ctx, r.database)

	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM product_configurable_attribute WHERE product_id = ? AND configurable_attribute_id = ?", productID, attributeID).Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		_, err := db.ExecContext(ctx, "INSERT INTO product_configurable_attribute (product_id, configurable_attribute_id) VALUES (?, ?)", productID, attributeID)
		return err
	}
	return nil
}

func (r *Repository) getOrCreateDefaultGroupTx(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	name := "Deliveroo Options"
	// configurable_attributes.id is a varchar PK with no default (same as
	// menu.CreateAttribute / webhook/ubereats.CreateAttributeFromUberGroup) —
	// it is never an auto-increment integer, so the previous SELECT id INTO
	// int / LastInsertId() pair could never have worked.
	var id string
	err := db.QueryRowContext(ctx, "SELECT id FROM configurable_attributes WHERE merchant_id = ? AND name = ?", merchantID, name).Scan(&id)
	if err == nil {
		return id, nil
	}

	newID := helpers.GeneratePrefixedID(helpers.AttributeIDPrefix)

	// NOTE: configurable_attributes.product_id is also NOT NULL with no
	// default and is not set here — same pre-existing bug as
	// webhook/ubereats.CreateAttributeFromUberGroup (confirmed present in the
	// MySQL source DDL too, so this insert has likely never actually
	// succeeded in production). Left unfixed, see Tier2 report.
	_, err = db.ExecContext(ctx, `
		INSERT INTO configurable_attributes (id, merchant_id, brand, name, title, is_required, min_options, max_options)
		VALUES (?, ?, 'DELIVEROO', ?, 'Options Deliveroo', false, 0, 99)`,
		newID, merchantID, name)

	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
		return "", err
	}

	return newID, nil
}

// SyncProduct : Logique de synchronisation/création de produit
func (r *Repository) SyncProduct(ctx context.Context, merchantID string, item DeliverooItem) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	// products.product_id is an integer identity while
	// integration_deliveroo_products_mapping.product_id is varchar — same
	// cross-type join issue as GetMerchantByLocationID above.
	productIDCast := "CAST(p.product_id AS CHAR)"
	if dbx.ActiveDialect() == dbx.Postgres {
		productIDCast = "CAST(p.product_id AS TEXT)"
	}

	// 1. Check Mapping
	queryMap := fmt.Sprintf(`
		SELECT p.product_id
		FROM products p
		INNER JOIN integration_deliveroo_products_mapping map on %s = map.product_id
		WHERE map.item_id = ? AND map.merchant_id = ? AND map.enabled = '1' AND p.enabled = '1'`, productIDCast)
	var productID string
	err := db.QueryRowContext(ctx, queryMap, item.PosItemID, merchantID).Scan(&productID)
	if err == nil {
		return productID, nil
	}

	newID, err := db.InsertReturningID(ctx,
		`INSERT INTO products (merchant_id, name, product_desc, price) VALUES(?, ?, ?, ?)`,
		"product_id", merchantID, item.Name, item.OperationalName, item.UnitPrice.Fractional)
	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
		return "", err
	}
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
	db := dbx.GetDB(ctx, r.database)

	// brand_status s'écrit toujours en majuscules (B3) : Deliveroo envoie ses
	// statuts en minuscules ("rejected", "canceled"), seul provider de ce
	// dépôt à le faire.
	query := `
		UPDATE orders
		SET brand_status = ?, state = 'CLOSED', merchant_approval = 'DENIED'
		WHERE brand_order_id = ?`
	_, err := db.ExecContext(ctx, query, strings.ToUpper(status), brandOrderID)
	return err
}

// DisablePayments désactive les paiements pour une commande annulée
func (r *Repository) DisablePayments(ctx context.Context, brandOrderID string) error {
	db := dbx.GetDB(ctx, r.database)

	// MySQL's UPDATE...JOIN has no direct Postgres equivalent; Postgres uses
	// UPDATE...FROM instead.
	query := `
		UPDATE payments p
		JOIN orders o ON p.order_id = o.order_id
		SET p.enabled = FALSE
		WHERE o.brand_order_id = ?`
	if dbx.ActiveDialect() == dbx.Postgres {
		query = `
		UPDATE payments
		SET enabled = FALSE
		FROM orders
		WHERE payments.order_id = orders.order_id AND orders.brand_order_id = ?`
	}
	_, err := db.ExecContext(ctx, query, brandOrderID)

	return err
}

// UpdateOrderAccepted met à jour le statut ACCEPTED (avec logique toggle scheduled du PHP)
func (r *Repository) UpdateOrderAccepted(ctx context.Context, brandOrderID string, isScheduledToggle bool) error {
	db := dbx.GetDB(ctx, r.database)

	var query string
	if isScheduledToggle {
		// Logique PHP: case WHEN brand_status = 'scheduled' then 'accepted' else 'scheduled' end
		query = `
			UPDATE orders
			SET brand_status = CASE WHEN brand_status = 'SCHEDULED' THEN 'ACCEPTED' ELSE 'SCHEDULED' END,
			    merchant_approval = 'ACCEPTED'
			WHERE brand_order_id = ?`
	} else {
		query = `
			UPDATE orders
			SET brand_status = 'ACCEPTED', merchant_approval = 'ACCEPTED'
			WHERE brand_order_id = ?`
	}
	_, err := db.ExecContext(ctx, query, brandOrderID)

	return err
}

// UpdateOrderConfirmed met à jour le statut CONFIRMED
func (r *Repository) UpdateOrderConfirmed(ctx context.Context, brandOrderID string) error {
	db := dbx.GetDB(ctx, r.database)

	query := `
		UPDATE orders
		SET brand_status = 'CONFIRMED', merchant_approval = 'ACCEPTED'
		WHERE brand_order_id = ?`
	_, err := db.ExecContext(ctx, query, brandOrderID)
	return err
}

// GetOrderIDByBrandID récupère l'ID interne (order_id) via l'ID Deliveroo
func (r *Repository) GetOrderIDByBrandIDTx(ctx context.Context, brandOrderID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `SELECT order_id FROM orders WHERE brand_order_id = ?`
	var orderID string
	err := db.QueryRowContext(ctx, query, brandOrderID).Scan(&orderID)
	if err != nil {
		return "", err
	}
	return orderID, nil
}

func (r *Repository) GetOrderIDByBrandID(ctx context.Context, brandOrderID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

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
	db := dbx.GetDB(ctx, r.database)

	// products.product_id is an integer identity while
	// integration_deliveroo_products_mapping.product_id is varchar — same
	// cross-type join issue as SyncProduct/GetMerchantByLocationID above.
	productIDCast := "CAST(p.product_id AS CHAR)"
	if dbx.ActiveDialect() == dbx.Postgres {
		productIDCast = "CAST(p.product_id AS TEXT)"
	}
	query := fmt.Sprintf(`
        SELECT map.item_name, map.item_id
        FROM integration_deliveroo_products_mapping map
        INNER JOIN products p ON %s = map.product_id
        WHERE map.item_id = ?
        AND map.merchant_id = ?
    `, productIDCast)

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
