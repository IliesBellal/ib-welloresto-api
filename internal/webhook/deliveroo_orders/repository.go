package deliveroo_orders

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
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
//
// Le chemin courant (option déjà mappée) est une simple lecture. Une option inconnue
// est créée avec son mapping dans une transaction qui tient un verrou propre à
// l'option : deux commandes simultanées ne la créent pas deux fois (le second mapping
// échouait sur l'index unique (merchant_id, item_id)) et un échec ne laisse pas
// d'option sans mapping.
func (r *Repository) SyncOption(ctx context.Context, merchantID string, productID string, mod DeliverooModifier) (string, string, error) {
	// 1. Essayer de trouver le mapping
	if attributeID, optionID, found, err := r.findOptionMapping(ctx, merchantID, mod.PosItemID); err == nil && found {
		// Trouvé, mais il faut s'assurer que le produit est lié à l'attribut (logique du PHP step 2)
		if errLink := r.ensureProductAttributeLink(ctx, productID, attributeID); errLink != nil {
			return "", "", errLink
		}
		return attributeID, optionID, nil
	}

	var attributeID, optionID string
	err := dbutils.RunInTxLocked(ctx, r.database, "deliveroo-option:"+merchantID+":"+mod.PosItemID, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.database)

		// Relecture sous verrou : une commande simultanée a pu la créer entre-temps.
		attrID, optID, found, err := r.findOptionMapping(txCtx, merchantID, mod.PosItemID)
		if err != nil {
			return err
		}
		if found {
			attributeID, optionID = attrID, optID
			return r.ensureProductAttributeLink(txCtx, productID, attributeID)
		}

		// a. Get or Create Default Group
		attributeID, err = r.getOrCreateDefaultGroupTx(txCtx, merchantID, productID)
		if err != nil {
			return err
		}

		// b. Create Option (configurable_attribute_options.title : 80 caractères)
		optIDInt, err := db.InsertReturningID(txCtx, `
			INSERT INTO configurable_attribute_options (configurable_attribute_id, title, extra_price, display_order)
			VALUES (?, ?, ?, (SELECT COALESCE(MAX(display_order) + 1, 0) FROM configurable_attribute_options WHERE configurable_attribute_id = ?))`,
			"id", attributeID, helpers.TruncateRunes(mod.Name, 80), mod.UnitPrice.Fractional, attributeID)
		if err != nil {
			return err
		}
		optionID = strconv.FormatInt(optIDInt, 10)

		// c. Create Mapping
		if _, err = db.ExecContext(txCtx, `
			INSERT INTO integration_deliveroo_options_mapping (merchant_id, configurable_attribute_option_id, item_id)
			VALUES (?, ?, ?)`,
			merchantID, optionID, mod.PosItemID); err != nil {
			return err
		}

		// d. Link Product to Attribute (si pas fait)
		return r.ensureProductAttributeLink(txCtx, productID, attributeID)
	})
	if err != nil {
		return "", "", err
	}
	return attributeID, optionID, nil
}

// findOptionMapping lit le mapping d'une option Deliveroo. found vaut false, sans
// erreur, quand l'option n'est pas encore connue.
func (r *Repository) findOptionMapping(ctx context.Context, merchantID, itemID string) (attributeID, optionID string, found bool, err error) {
	db := dbx.GetDB(ctx, r.database)
	err = db.QueryRowContext(ctx, `
		SELECT opt.id AS option_id, opt.configurable_attribute_id AS attribute_id
		FROM configurable_attribute_options opt
		JOIN integration_deliveroo_options_mapping map ON opt.id = map.configurable_attribute_option_id
		WHERE map.item_id = ? AND map.merchant_id = ?`, itemID, merchantID).Scan(&optionID, &attributeID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return attributeID, optionID, true, nil
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

// getOrCreateDefaultGroupTx renvoie le groupe d'options Deliveroo du produit,
// créé à la première option inconnue. configurable_attributes.product_id est
// obligatoire : un groupe appartient à un produit (les autres produits qui
// utilisent la même option s'y rattachent par ensureProductAttributeLink). Sans
// product_id, l'insertion échouait et la commande Deliveroo n'était pas créée.
func (r *Repository) getOrCreateDefaultGroupTx(ctx context.Context, merchantID, productID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	productIDInt, err := strconv.Atoi(productID)
	if err != nil {
		return "", fmt.Errorf("deliveroo options group: invalid product id %q: %w", productID, err)
	}

	name := "Deliveroo Options"
	// configurable_attributes.id is a varchar PK with no default (same as
	// menu.CreateAttribute / webhook/ubereats.CreateAttributeFromUberGroup) —
	// it is never an auto-increment integer, so the previous SELECT id INTO
	// int / LastInsertId() pair could never have worked.
	var id string
	err = db.QueryRowContext(ctx, "SELECT id FROM configurable_attributes WHERE merchant_id = ? AND name = ? AND product_id = ?", merchantID, name, productIDInt).Scan(&id)
	if err == nil {
		return id, nil
	}

	newID := helpers.GeneratePrefixedID(helpers.AttributeIDPrefix)

	_, err = db.ExecContext(ctx, `
		INSERT INTO configurable_attributes (id, product_id, merchant_id, brand, name, title, is_required, min_options, max_options)
		VALUES (?, ?, ?, 'DELIVEROO', ?, 'Options Deliveroo', false, 0, 99)`,
		newID, productIDInt, merchantID, name)

	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
		return "", err
	}

	return newID, nil
}

// SyncProduct : Logique de synchronisation/création de produit
//
// Le chemin courant (produit déjà mappé) est une simple lecture. Un article inconnu
// est créé avec son mapping dans une transaction qui tient un verrou propre à
// l'article : deux commandes simultanées ne créent pas deux produits (la clé du
// mapping contient l'identifiant du produit, rien n'empêchait le doublon) et un
// échec ne laisse pas de produit sans mapping.
func (r *Repository) SyncProduct(ctx context.Context, merchantID string, item DeliverooItem) (string, error) {
	// 1. Check Mapping
	if productID, found, err := r.findProductMapping(ctx, merchantID, item.PosItemID); err == nil && found {
		return productID, nil
	}

	var productID string
	err := dbutils.RunInTxLocked(ctx, r.database, "deliveroo-product:"+merchantID+":"+item.PosItemID, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.database)

		// Relecture sous verrou : une commande simultanée a pu le créer entre-temps.
		existing, found, err := r.findProductMapping(txCtx, merchantID, item.PosItemID)
		if err != nil {
			return err
		}
		if found {
			productID = existing
			return nil
		}

		// Deliveroo n'envoie aucun taux de TVA : 10 %, ou 20 % pour une boisson
		// alcoolisée reconnue à son nom. Sans cela les trois colonnes tombaient au
		// défaut de la table (0 = non défini, 0 %).
		tva := models.MarketplaceTVAFor(models.LooksAlcoholic(item.Name) || models.LooksAlcoholic(item.OperationalName))
		newID, err := db.InsertReturningID(txCtx,
			`INSERT INTO products (merchant_id, name, product_desc, price, category, tva_in_id, tva_take_away_id, tva_delivery_id) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
			"product_id", merchantID, helpers.TruncateRunes(item.Name, 255), item.OperationalName, item.UnitPrice.Fractional, models.MarketplaceCategoryDeliveroo,
			tva.In, tva.TakeAway, tva.Delivery)
		if err != nil {
			logger.FromContext(ctx).Error(err.Error())
			return err
		}
		productID = strconv.FormatInt(newID, 10)

		if _, err = db.ExecContext(txCtx, `INSERT INTO integration_deliveroo_products_mapping(merchant_id, product_id, item_id, item_name) VALUES(?, ?, ?, ?)`, merchantID, productID, item.PosItemID, item.OperationalName); err != nil {
			logger.FromContext(ctx).Error(err.Error())
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return productID, nil
}

// findProductMapping lit le produit mappé à un article Deliveroo. found vaut false,
// sans erreur, quand l'article n'est pas encore connu.
func (r *Repository) findProductMapping(ctx context.Context, merchantID, itemID string) (productID string, found bool, err error) {
	db := dbx.GetDB(ctx, r.database)

	// products.product_id is an integer identity while
	// integration_deliveroo_products_mapping.product_id is varchar — same
	// cross-type join issue as GetMerchantByLocationID above.
	productIDCast := "CAST(p.product_id AS CHAR)"
	if dbx.ActiveDialect() == dbx.Postgres {
		productIDCast = "CAST(p.product_id AS TEXT)"
	}

	err = db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p.product_id
		FROM products p
		INNER JOIN integration_deliveroo_products_mapping map on %s = map.product_id
		WHERE map.item_id = ? AND map.merchant_id = ? AND map.enabled = '1' AND p.enabled = '1'`, productIDCast),
		itemID, merchantID).Scan(&productID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return productID, true, nil
}

// --- Status Update Logic (Transaction Based) ---

// Le refus et l'annulation Deliveroo passent par
// order_life_cycle.DeleteOrder (paiements annulés par fiscal.CancelOrderPayments,
// lot C conformité caisse) : UpdateOrderRejected et DisablePayments, sans
// appelant, ont été supprimés (ils écrivaient sans contrôle ni trace).

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
