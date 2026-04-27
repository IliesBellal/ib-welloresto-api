package stocks

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"

	"go.uber.org/zap"
)

type StocksRepository struct {
	database *sql.DB
}

func NewStockRepository(db *sql.DB) *StocksRepository {
	return &StocksRepository{database: db}
}

func (r *StocksRepository) GetBarcodeInfo(ctx context.Context, merchantID, code string) (*models.ComponentBarcodeInfo, []models.AvailableUOM, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
        SELECT bc.component_id, c.name, quantity, bc.uom, price, uomd.uom_desc
        FROM barcodes bc
        INNER JOIN components c 
            ON bc.component_id = c.component_id 
            AND bc.merchant_id = c.merchant_id
        LEFT JOIN unit_of_measure_desc uomd 
            ON uomd.id = bc.uom AND uomd.lang='FR'
        WHERE bc.merchant_id = ? AND barcode = ?;
    `

	row := db.QueryRowContext(ctx, query, merchantID, code)
	var info models.ComponentBarcodeInfo

	err := row.Scan(
		&info.ComponentID,
		&info.ComponentName,
		&info.Quantity,
		&info.UOM,
		&info.Price,
		&info.UOMDesc,
	)

	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	// Fetch Available UOMs
	convertQuery := `
        SELECT id_to, uom_desc
        FROM components c
        INNER JOIN unit_of_measure_convert uomc ON c.unit_of_measure = uomc.id_from
        INNER JOIN unit_of_measure_desc uomd ON uomd.id = uomc.id_to AND uomd.lang = 'FR'
        WHERE component_id = ?;
    `

	rows, err := db.QueryContext(ctx, convertQuery, info.ComponentID)
	if err != nil {
		return &info, nil, err
	}
	defer rows.Close()

	available := []models.AvailableUOM{}

	for rows.Next() {
		var a models.AvailableUOM
		if err := rows.Scan(&a.UOMID, &a.UOMDesc); err != nil {
			return &info, nil, err
		}
		available = append(available, a)
	}

	return &info, available, nil
}

func (r *StocksRepository) DeleteBarcode(ctx context.Context, merchantID, code string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx,
		`DELETE FROM barcodes WHERE barcode = ? AND merchant_id = ?`,
		code, merchantID,
	)
	return err
}

func (r *StocksRepository) CreateBarcode(ctx context.Context, merchantID, code, componentID string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx,
		`INSERT INTO barcodes(merchant_id, barcode, component_id) VALUES (?, ?, ?)`,
		merchantID, code, componentID,
	)
	return err
}

// AddStockBarcode: implémente la logique transactionnelle équivalente à addStockBarcode PHP.
// Retourne nil si OK, ou une erreur.
func (r *StocksRepository) AddStockBarcode(ctx context.Context, merchantID string, userID string, barcode string, s models.BarcodeSpecs) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1) Update barcodes
	_, err := db.ExecContext(ctx, `
		UPDATE barcodes
		SET last_scan = UTC_TIMESTAMP,
		    price = ?,
		    quantity = ?,
		    uom = ?
		WHERE barcode = ?
		  AND merchant_id = ?;
	`, s.BCPrice, s.BCQuantity, s.BCUOM, barcode, merchantID)
	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
		return err
	}

	// 2) Update components stock using unit_of_measure_convert ratio
	// stock = ROUND(stock + (c_quantity * bc_quantity * ratio), 4)
	_, err = db.ExecContext(ctx, `
		UPDATE components
		INNER JOIN unit_of_measure_convert ON components.unit_of_measure = unit_of_measure_convert.id_from
			AND unit_of_measure_convert.id_to = ?
		SET stock = ROUND(stock + (? * ? * unit_of_measure_convert.ratio), 4),
		    status = CASE WHEN ROUND(stock + (? * ? * unit_of_measure_convert.ratio),4) > 0 THEN 1 ELSE status END
		WHERE component_id = ?;
	`, s.BCUOM, s.CQuantity, s.BCQuantity, s.CQuantity, s.BCQuantity, s.ComponentID)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	// 3) Update components purchase price info when auto_update_purchase_info is true
	// purchase_price = CAST(bc_price as DECIMAL(5,3))*100,
	// purchase_price_quantity = round(bc_quantity * ratio,4)
	_, err = db.ExecContext(ctx, `
		UPDATE components
		INNER JOIN unit_of_measure_convert ON components.unit_of_measure = unit_of_measure_convert.id_from
			AND unit_of_measure_convert.id_to = ?
		SET purchase_price = ? * 100,
		    purchase_price_quantity = ROUND(? * unit_of_measure_convert.ratio, 4)
		WHERE component_id = ?
		  AND auto_update_purchase_info IS TRUE;
	`, s.BCUOM, s.BCPrice, s.BCQuantity, s.ComponentID)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	// 4) Insert stock_movements
	_, err = db.ExecContext(ctx, `
		INSERT INTO stock_movements(id, merchant_id, user_id, component_id, source, movement, quantity, unit_of_measure, movement_date)
		VALUES (?, ?, ?, ?, 'scan', 'add', ? * ?, ?, UTC_TIMESTAMP)
	`, helpers.GeneratePrefixedID("stck-mvt"), merchantID, userID, s.ComponentID, s.CQuantity, s.BCQuantity, s.BCUOM)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	// 5) Insert purchased_components
	// VALUES(:merchant_id, :component_id, :barcode, :bc_price, :bc_quantity, :bc_quantity * :c_quantity, :bc_uom, :c_quantity, UTC_TIMESTAMP)
	res, err := db.ExecContext(ctx, `
		INSERT INTO purchased_components(
			merchant_id, component_id, barcode, price, quantity, remaining_quantity, uom, bought_quantity, registration_date
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP)
	`, merchantID, s.ComponentID, barcode, s.BCPrice, s.BCQuantity, s.BCQuantity*s.CQuantity, s.BCUOM, s.CQuantity)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	purchasedComponentID, err := res.LastInsertId()
	if err != nil {
		log.Error(err.Error())
		return err
	}

	// 6) If DLC provided -> insert expiration_dates
	if s.DLC != nil && *s.DLC != "" {
		// Try to parse date to ensure valid format (YYYY-MM-DD or YYYY-MM-DD HH:MM:SS accepted by DB)
		_, perr := time.Parse("2006-01-02", *s.DLC)
		if perr != nil {
			// try full timestamp parse
			_, perr2 := time.Parse("2006-01-02 15:04:05", *s.DLC)
			if perr2 != nil {
				// keep it as-is but log a warning; still attempt to insert
				log.Warn("invalid dlc format, inserting raw value", zap.String("dlc", *s.DLC))
			}
		}

		_, err = db.ExecContext(ctx, `
			INSERT INTO expiration_dates(id, merchant_id, component_id, expiration_date, creation_date, purchased_component_id)
			VALUES (?, ?, ?, ?, UTC_TIMESTAMP, ?)
		`, helpers.GeneratePrefixedID("exp-date"), merchantID, s.ComponentID, *s.DLC, purchasedComponentID)
		if err != nil {
			log.Error(err.Error())
			return err
		}
	}

	return nil
}

func (r *StocksRepository) SetStockLoss(ctx context.Context, merchantID string, userID string, req models.StockLossRequest) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	switch req.Type {

	case "COMPONENT":
		// 1️⃣ UPDATE COMPONENT STOCK
		_, err := db.ExecContext(ctx, `
            UPDATE components c
            INNER JOIN unit_of_measure_convert uomc 
                ON c.unit_of_measure = uomc.id_from AND uomc.id_to = ?
            INNER JOIN merchant_parameters mp ON mp.merchant_id = c.merchant_id
            SET 
                c.stock = ROUND(c.stock - ROUND(? * uomc.ratio, 4), 4),
                c.status = CASE 
                    WHEN mp.disable_components_under_safety_stock
                         AND (c.stock - ROUND(? * uomc.ratio,4)) < c.safety_stock
                         AND c.safety_triggered = 0 
                    THEN 0 ELSE c.status END,
                c.safety_triggered = CASE
                    WHEN mp.disable_components_under_safety_stock
                         AND (c.stock - ROUND(? * uomc.ratio,4)) < c.safety_stock
                         AND c.safety_triggered = 0
                    THEN 1 ELSE c.safety_triggered END
            WHERE c.component_id = ?;
        `, req.UOM, req.Qty, req.Qty, req.Qty, req.ObjectID)
		if err != nil {
			log.Error(err.Error())
			return err
		}

		// 2️⃣ INSERT MOVEMENT
		_, err = db.ExecContext(ctx, `
            INSERT INTO stock_movements
                (id, merchant_id, user_id, component_id, product_id, source, movement, quantity, unit_of_measure, order_item_id, comment)
            SELECT ?, u.merchant_id, u.user_id, ?, NULL, 'manual', 'loss', ?, ?, NULL, ?
            FROM users u
            WHERE u.user_id = ?;
        `, helpers.GeneratePrefixedID("stck-mvt"), req.ObjectID, req.Qty, req.UOM, req.Comment, userID)
		if err != nil {
			log.Error(err.Error())
			return err
		}

	case "PRODUCT":

		// 1️⃣ GET COMPONENTS IMPACTED BY THE PRODUCT LOSS
		rows, err := db.QueryContext(ctx, `
            SELECT 
                c.merchant_id,
                p.product_id,
                c.component_id,
                ROUND(rq.quantity * uomc.ratio * ?, 4) as qty,
                rq.unit_of_measure
            FROM products p
            INNER JOIN recipes r ON r.product_id = p.product_id
            INNER JOIN requires rq ON rq.recipe_id = r.recipe_id AND rq.enabled IS TRUE
            INNER JOIN components c ON c.component_id = rq.component_id
            INNER JOIN unit_of_measure_convert uomc 
                ON uomc.id_from = c.unit_of_measure AND uomc.id_to = rq.unit_of_measure
            WHERE p.product_id = ?;
        `, req.Qty, req.ObjectID)
		if err != nil {
			log.Error(err.Error())
			return err
		}

		type rowItem struct {
			MerchantID  string
			ProductID   string
			ComponentID string
			Qty         float64
			UOM         string
		}

		var items []rowItem
		for rows.Next() {
			var it rowItem
			if err := rows.Scan(&it.MerchantID, &it.ProductID, &it.ComponentID, &it.Qty, &it.UOM); err != nil {
				log.Error(err.Error())
				return err
			}
			items = append(items, it)
		}
		rows.Close()

		// 2️⃣ APPLY STOCK LOSS FOR EACH COMPONENT
		for _, it := range items {
			_, err = db.ExecContext(ctx, `
                UPDATE components
                SET stock = ROUND(stock - ?, 4)
                WHERE component_id = ?;
            `, it.Qty, it.ComponentID)
			if err != nil {
				log.Error(err.Error())
				return err
			}

			_, err = db.ExecContext(ctx, `
                INSERT INTO stock_movements(
                    id, merchant_id, user_id, component_id, product_id, source, movement, quantity, unit_of_measure, order_item_id, comment
                )
                VALUES (?, ?, ?, ?, ?, 'manual', 'loss', ?, ?, NULL, ?);
            `, helpers.GeneratePrefixedID("stck-mvt"), it.MerchantID, userID, it.ComponentID, it.ProductID, it.Qty, it.UOM, req.Comment)
			if err != nil {
				log.Error(err.Error())
				return err
			}
		}

	default:
		return errors.New("invalid type for stock loss")
	}

	return nil
}

func (r *StocksRepository) GetStockProducts(ctx context.Context, merchantID string, t string) ([]models.StockCategory, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var qCateg, qObjects string

	switch t {
	case "PRODUCT":
		qCateg = `
            SELECT merchant_categ_id, categ_name
            FROM productcateg
            WHERE available = 1 AND merchant_id = ?
            ORDER BY categ_order ASC;
        `
		qObjects = `
            SELECT product_id, name, category, 1 as unit_of_measure
            FROM products
            WHERE merchant_id = ? AND available = 1;
        `

	case "COMPONENT":
		qCateg = `
            SELECT merchant_categ_id, name
            FROM component_category
            WHERE merchant_id = ? AND available = 1
            ORDER BY categ_order ASC;
        `
		qObjects = `
            SELECT component_id, name, category_id, unit_of_measure
            FROM components
            WHERE merchant_id = ? AND category_id <> 'UBER_EATS_TEMP';
        `
	default:
		return nil, errors.New("invalid type")
	}

	// --- Load UOM ---
	resUOM, err := db.QueryContext(ctx, `
        SELECT uom.id, id_to, uomd.uom_desc
        FROM unit_of_measure uom
        INNER JOIN unit_of_measure_convert uomc ON uom.id = uomc.id_from
        INNER JOIN unit_of_measure_desc uomd ON uom.id = uomd.id
        WHERE uomd.lang = 'FR';
    `)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	type UOM struct {
		ID      string
		CheckID string
		Desc    string
	}

	var uoms []UOM
	for resUOM.Next() {
		var u UOM
		resUOM.Scan(&u.ID, &u.CheckID, &u.Desc)
		uoms = append(uoms, u)
	}
	resUOM.Close()

	// --- Load objects ---
	resObj, err := db.QueryContext(ctx, qObjects, merchantID)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	type obj struct {
		ID    string
		Name  string
		Categ string
		UOM   string
	}

	var objs []obj
	for resObj.Next() {
		var o obj
		resObj.Scan(&o.ID, &o.Name, &o.Categ, &o.UOM)
		objs = append(objs, o)
	}
	resObj.Close()

	// assign UOM to each object
	finalObjects := make(map[string][]models.StockObject)
	for _, o := range objs {
		var actual []models.UOMEntry
		for _, uu := range uoms {
			if uu.CheckID == o.UOM {
				actual = append(actual, models.UOMEntry{
					UOMID:   uu.ID,
					UOMDesc: uu.Desc,
				})
			}
		}

		finalObjects[o.Categ] = append(finalObjects[o.Categ], models.StockObject{
			ObjectID:   o.ID,
			ObjectName: o.Name,
			UOM:        actual,
		})
	}

	// --- Load categories ---
	resCateg, err := db.QueryContext(ctx, qCateg, merchantID)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	var cats []models.StockCategory
	for resCateg.Next() {
		var id, name string
		resCateg.Scan(&id, &name)

		cats = append(cats, models.StockCategory{
			CategoryID:   id,
			CategoryName: name,
			Objects:      finalObjects[id],
		})
	}
	resCateg.Close()

	return cats, nil
}

func (r *StocksRepository) GetComponentsList(ctx context.Context, merchantID string) ([]models.StockComponentListItem, error) {
	db := dbutils.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
		SELECT
			c.component_id,
			c.name,
			COALESCE(c.unit_of_measure, ''),
			COALESCE(uomd.uom_desc, ''),
			COALESCE(uomd.uom_short_desc, ''),
			c.stock,
			c.safety_stock,
			c.purchase_price,
			c.purchase_price_quantity
		FROM components c
		LEFT JOIN unit_of_measure_desc uomd
			ON uomd.id = c.unit_of_measure AND uomd.lang = 'FR'
		WHERE c.merchant_id = ?
		  AND c.enabled = 1
		  AND c.category_id <> 'UBER_EATS_TEMP'
		ORDER BY c.name ASC
	`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	components := make([]models.StockComponentListItem, 0)
	for rows.Next() {
		var item models.StockComponentListItem
		var unitID, unitName, unitShortName string
		var purchasePrice sql.NullInt64
		var purchasePriceQuantity sql.NullFloat64

		if err := rows.Scan(
			&item.ComponentID,
			&item.Name,
			&unitID,
			&unitName,
			&unitShortName,
			&item.Quantity,
			&item.AlertThreshold,
			&purchasePrice,
			&purchasePriceQuantity,
		); err != nil {
			return nil, err
		}

		item.Unit = models.StockComponentListUnit{UnitID: unitID, UnitName: unitName, UnitShortName: unitShortName}
		item.PurchasingPrice = 0
		if purchasePrice.Valid && purchasePriceQuantity.Valid && purchasePriceQuantity.Float64 > 0 {
			item.PurchasingPrice = helpers.RoundToNearestInt(float64(purchasePrice.Int64) / purchasePriceQuantity.Float64)
		}

		components = append(components, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return components, nil
}

// RecordComponentMovement inserts a manual stock movement for a component and adjusts its stock.
func (r *StocksRepository) RecordComponentMovement(ctx context.Context, merchantID, userID string, req StockComponentMovementRequest) error {
	db := dbutils.GetDB(ctx, r.database)

	// 1. Validate component belongs to this merchant.
	var exists int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM components WHERE component_id = ? AND merchant_id = ?`,
		req.ComponentID, merchantID,
	).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrComponentNotFound
	}

	// 2. Validate the requested unit has a conversion path for this component.
	var ratio float64
	err := db.QueryRowContext(ctx, `
		SELECT uomc.ratio
		FROM components c
		INNER JOIN unit_of_measure_convert uomc
			ON c.unit_of_measure = uomc.id_from AND uomc.id_to = ?
		WHERE c.component_id = ?
	`, req.Unit, req.ComponentID).Scan(&ratio)
	if err == sql.ErrNoRows {
		return ErrUnitNotFound
	}
	if err != nil {
		return err
	}

	// 3. Map movement type to its DB code and stock sign.
	var movementCode string
	var addToStock bool
	switch req.Type {
	case "add":
		movementCode = "add"
		addToStock = true
	case "remove":
		movementCode = "remove"
		addToStock = false
	case "loss":
		movementCode = "loss"
		addToStock = false
	default:
		return ErrInvalidMovement
	}

	// 4. Update component stock (convert quantity to the component's native unit via ratio).
	if addToStock {
		_, err = db.ExecContext(ctx, `
			UPDATE components c
			INNER JOIN unit_of_measure_convert uomc
				ON c.unit_of_measure = uomc.id_from AND uomc.id_to = ?
			SET c.stock = ROUND(c.stock + ROUND(? * uomc.ratio, 4), 4)
			WHERE c.component_id = ? AND c.merchant_id = ?
		`, req.Unit, req.Quantity, req.ComponentID, merchantID)
	} else {
		_, err = db.ExecContext(ctx, `
			UPDATE components c
			INNER JOIN unit_of_measure_convert uomc
				ON c.unit_of_measure = uomc.id_from AND uomc.id_to = ?
			SET c.stock = ROUND(c.stock - ROUND(? * uomc.ratio, 4), 4)
			WHERE c.component_id = ? AND c.merchant_id = ?
		`, req.Unit, req.Quantity, req.ComponentID, merchantID)
	}
	if err != nil {
		return err
	}

	// 5. Insert the stock movement record.
	// source = 'manual', quantity and unit stored as received (not converted).
	_, err = db.ExecContext(ctx, `
		INSERT INTO stock_movements
			(id, merchant_id, user_id, component_id, source, movement, quantity, unit_of_measure, comment)
		VALUES (?, ?, ?, ?, 'manual', ?, ?, ?, ?)
	`, helpers.GeneratePrefixedID("stck-mvt"), merchantID, userID, req.ComponentID, movementCode, req.Quantity, req.Unit, req.Comment)
	return err
}

// ConsumeOrderStock deducts components stock for all items in a closed order.
// Withouts are excluded (the customer removed that ingredient, so no consumption).
// Extras are ignored for now.
// Errors are non-fatal: the caller should log and continue.
//
// Stock deduction is in the component's native unit (required to update components.stock correctly).
// The movement row is recorded in the recipe unit (rq.unit_of_measure) so that it stays
// consistent with the recipe definition and is human-readable in stock reports.
func (r *StocksRepository) ConsumeOrderStock(ctx context.Context, merchantID, userID, orderID string) error {
	db := dbutils.GetDB(ctx, r.database)

	// Two quantities per row:
	//   deduct_qty   = rq.quantity * uomc.ratio * oi.quantity  → component native unit (for stock update)
	//   movement_qty = rq.quantity * oi.quantity               → recipe unit (for movement record)
	rows, err := db.QueryContext(ctx, `
		SELECT
			oi.order_item_id,
			oi.product_id,
			c.component_id,
			ROUND(rq.quantity * uomc.ratio * oi.quantity, 4) AS deduct_qty,
			ROUND(rq.quantity * oi.quantity, 4)              AS movement_qty,
			rq.unit_of_measure                               AS movement_uom
		FROM orderitems oi
		INNER JOIN recipes r
			ON r.product_id = oi.product_id
		INNER JOIN requires rq
			ON rq.recipe_id = r.recipe_id AND rq.enabled = TRUE
		INNER JOIN components c
			ON c.component_id = rq.component_id AND c.merchant_id = ?
		INNER JOIN unit_of_measure_convert uomc
			ON uomc.id_from = c.unit_of_measure AND uomc.id_to = rq.unit_of_measure
		WHERE oi.order_id = ?
		  AND NOT EXISTS (
			  SELECT 1 FROM without w
			  WHERE w.order_item_id = oi.order_item_id
			    AND w.component_id = rq.component_id
		  )
	`, merchantID, orderID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type consumptionRow struct {
		OrderItemID string
		ProductID   string
		ComponentID string
		DeductQty   float64 // in component's native unit — used to update components.stock
		MovementQty float64 // in recipe unit — stored in stock_movements
		MovementUOM string  // recipe unit id — stored in stock_movements
	}

	var items []consumptionRow
	for rows.Next() {
		var cr consumptionRow
		if err := rows.Scan(&cr.OrderItemID, &cr.ProductID, &cr.ComponentID, &cr.DeductQty, &cr.MovementQty, &cr.MovementUOM); err != nil {
			return err
		}
		items = append(items, cr)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(items) == 0 {
		return nil
	}

	for _, cr := range items {
		// Deduct from component stock using the native unit quantity.
		if _, err := db.ExecContext(ctx, `
			UPDATE components
			SET stock = ROUND(stock - ?, 4)
			WHERE component_id = ? AND merchant_id = ?
		`, cr.DeductQty, cr.ComponentID, merchantID); err != nil {
			return err
		}

		// Record the movement in the recipe's unit for human-readable traceability.
		if _, err := db.ExecContext(ctx, `
			INSERT INTO stock_movements
				(id, merchant_id, user_id, component_id, product_id, source, movement, quantity, unit_of_measure, order_item_id, order_id)
			VALUES (?, ?, ?, ?, ?, 'order', 'consume', ?, ?, ?, ?)
		`, helpers.GeneratePrefixedID("stck-mvt"), merchantID, userID, cr.ComponentID, cr.ProductID, cr.MovementQty, cr.MovementUOM, cr.OrderItemID, orderID); err != nil {
			return err
		}
	}

	return nil
}

// GetMovements fetches all component stock movements for a merchant between two dates (inclusive, UTC).
// Type is derived:
//   - movement '1'                           → "add"
//   - movement '2' + product_id IS NULL      → "remove"
//   - movement '2' + product_id IS NOT NULL  → "consumption"
//   - movement '4'                           → "loss"
func (r *StocksRepository) GetMovements(ctx context.Context, merchantID, from, to string) ([]StockMovementItem, error) {
	db := dbutils.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
		SELECT
			sm.id,
			sm.component_id,
			COALESCE(c.name, sm.component_id)                   AS component_name,
			COALESCE(c.unit_of_measure, sm.unit_of_measure)     AS unit_id,
			COALESCE(uomd.uom_desc, sm.unit_of_measure)         AS unit_name,
			COALESCE(uomd.uom_short_desc, sm.unit_of_measure)         AS unit_short_name,
			sm.quantity,
			sm.movement,
			sm.product_id,
			p.name                                               AS product_name,
			DATE_FORMAT(sm.movement_date, '%Y-%m-%dT%H:%i:%s')  AS created_at,
			COALESCE(u.name, sm.user_id)                         AS created_by,
			sm.comment
		FROM stock_movements sm
		LEFT JOIN components c
			ON c.component_id = sm.component_id
		LEFT JOIN unit_of_measure_desc uomd
			ON uomd.id = c.unit_of_measure AND uomd.lang = 'FR'
		LEFT JOIN products p
			ON p.product_id = sm.product_id
		LEFT JOIN users u
			ON u.user_id = sm.user_id
		WHERE sm.merchant_id = ?
		  AND sm.component_id IS NOT NULL
		  AND DATE(sm.movement_date) BETWEEN ? AND ?
		ORDER BY sm.movement_date DESC
	`, merchantID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]StockMovementItem, 0)
	for rows.Next() {
		var (
			item          StockMovementItem
			unitID        string
			unitName      string
			unitShortName string
			movementCode  string
			productID     sql.NullString
			productName   sql.NullString
			comment       sql.NullString
		)
		if err := rows.Scan(
			&item.ID,
			&item.ComponentID,
			&item.ComponentName,
			&unitID,
			&unitName,
			&unitShortName,
			&item.Quantity,
			&movementCode,
			&productID,
			&productName,
			&item.CreatedAt,
			&item.CreatedBy,
			&comment,
		); err != nil {
			return nil, err
		}

		item.Unit = StockMovementUnit{UnitID: unitID, UnitName: unitName, UnitShortName: unitShortName}

		// Direct mapping: movement column now stores explicit text values.
		switch movementCode {
		case "add", "remove", "loss", "consume":
			item.Type = movementCode
		default:
			item.Type = movementCode
		}

		if productName.Valid && productName.String != "" {
			item.ProductName = &productName.String
		}
		if comment.Valid && comment.String != "" {
			item.Comment = &comment.String
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}
