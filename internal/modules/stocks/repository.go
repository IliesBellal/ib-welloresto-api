package stocks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type StocksRepository struct {
	database *sql.DB
}

func NewStockRepository(db *sql.DB) *StocksRepository {
	return &StocksRepository{database: db}
}

// round4 wraps a SQL expression in a ROUND(..., 4) call. Stock/ratio columns
// in this module are double precision/real, and Postgres's two-argument
// ROUND only accepts numeric (unlike MySQL, which accepts any numeric type) —
// the same gap already fixed once in stats.roundToIntExpr.
func round4(expr string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "ROUND(CAST(" + expr + " AS numeric), 4)"
	}
	return "ROUND(" + expr + ", 4)"
}

func (r *StocksRepository) GetBarcodeInfo(ctx context.Context, merchantID, code string) (*models.ComponentBarcodeInfo, []models.AvailableUOM, error) {
	db := dbx.GetDB(ctx, r.database)

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
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx,
		`DELETE FROM barcodes WHERE barcode = ? AND merchant_id = ?`,
		code, merchantID,
	)
	return err
}

func (r *StocksRepository) CreateBarcode(ctx context.Context, merchantID, code, componentID string) error {
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx,
		`INSERT INTO barcodes(merchant_id, barcode, component_id) VALUES (?, ?, ?)`,
		merchantID, code, componentID,
	)
	return err
}

// AddStockBarcode: implémente la logique transactionnelle équivalente à addStockBarcode PHP.
// Retourne nil si OK, ou une erreur.
func (r *StocksRepository) AddStockBarcode(ctx context.Context, merchantID string, userID string, barcode string, s models.BarcodeSpecs) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1) Update barcodes
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE barcodes
		SET last_scan = %s,
		    price = ?,
		    quantity = ?,
		    uom = ?
		WHERE barcode = ?
		  AND merchant_id = ?;
	`, dbx.UTCNow()), s.BCPrice, s.BCQuantity, s.BCUOM, barcode, merchantID)
	if err != nil {
		logger.FromContext(ctx).Error(err.Error())
		return err
	}

	// 2) Update components stock using unit_of_measure_convert ratio
	// stock = ROUND(stock + (c_quantity * bc_quantity * ratio), 4)
	// UPDATE...JOIN rewritten as a scalar subquery + EXISTS guard (portable
	// MySQL/Postgres): if no conversion path exists for this unit, the row is
	// left untouched, matching the original INNER JOIN's all-or-nothing match.
	// components.status is varchar, not boolean — the literal must stay '1'.
	// c_quantity*bc_quantity multiplied in Go: `? * ?` between two untyped
	// parameters is ambiguous to Postgres ("operator is not unique"), per the
	// Tier1 report's transversal note on arithmetic between bare parameters.
	scannedQty := s.CQuantity * s.BCQuantity
	newStockExpr := round4("stock + (? * (SELECT uomc.ratio FROM unit_of_measure_convert uomc WHERE uomc.id_from = components.unit_of_measure AND uomc.id_to = ?))")
	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE components
		SET stock = %[1]s,
		    status = CASE WHEN %[1]s > 0 THEN '1' ELSE status END
		WHERE component_id = ?
		  AND EXISTS (SELECT 1 FROM unit_of_measure_convert uomc WHERE uomc.id_from = components.unit_of_measure AND uomc.id_to = ?);
	`, newStockExpr), scannedQty, s.BCUOM, scannedQty, s.BCUOM, s.ComponentID, s.BCUOM)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	// 3) Update components purchase price info when auto_update_purchase_info is true
	// purchase_price = CAST(bc_price as DECIMAL(5,3))*100,
	// purchase_price_quantity = round(bc_quantity * ratio,4)
	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE components
		SET purchase_price = ? * 100,
		    purchase_price_quantity = %s
		WHERE component_id = ?
		  AND auto_update_purchase_info IS TRUE
		  AND EXISTS (SELECT 1 FROM unit_of_measure_convert uomc WHERE uomc.id_from = components.unit_of_measure AND uomc.id_to = ?);
	`, round4("? * (SELECT uomc.ratio FROM unit_of_measure_convert uomc WHERE uomc.id_from = components.unit_of_measure AND uomc.id_to = ?)")),
		s.BCPrice, s.BCQuantity, s.BCUOM, s.ComponentID, s.BCUOM)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	// 4) Insert stock_movements
	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO stock_movements(id, merchant_id, user_id, component_id, source, movement, quantity, unit_of_measure, movement_date)
		VALUES (?, ?, ?, ?, 'scan', 'add', ?, ?, %s)
	`, dbx.UTCNow()), helpers.GeneratePrefixedID(helpers.StockMovementPrefix), merchantID, userID, s.ComponentID, scannedQty, s.BCUOM)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	// 5) Insert purchased_components
	// VALUES(:merchant_id, :component_id, :barcode, :bc_price, :bc_quantity, :bc_quantity * :c_quantity, :bc_uom, :c_quantity, UTC_TIMESTAMP)
	purchasedComponentID, err := db.InsertReturningID(ctx, fmt.Sprintf(`
		INSERT INTO purchased_components(
			merchant_id, component_id, barcode, price, quantity, remaining_quantity, uom, bought_quantity, registration_date
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, %s)
	`, dbx.UTCNow()), "id", merchantID, s.ComponentID, barcode, s.BCPrice, s.BCQuantity, s.BCQuantity*s.CQuantity, s.BCUOM, s.CQuantity)
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

		// expiration_dates.id is an auto-increment identity column, not a
		// client-generated prefixed id (same class of issue as
		// locations.CreateFloor/CreateArea/CreateTable, fixed per that
		// established resolution: read back the generated id instead of
		// inserting a client-generated prefixed string).
		_, err = db.InsertReturningID(ctx, fmt.Sprintf(`
			INSERT INTO expiration_dates(merchant_id, component_id, expiration_date, creation_date, purchased_component_id)
			VALUES (?, ?, ?, %s, ?)
		`, dbx.UTCNow()), "id", merchantID, s.ComponentID, *s.DLC, purchasedComponentID)
		if err != nil {
			log.Error(err.Error())
			return err
		}
	}

	return nil
}

func (r *StocksRepository) SetStockLoss(ctx context.Context, merchantID string, userID string, req models.StockLossRequest) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	switch req.Type {

	case "COMPONENT":
		// 1️⃣ UPDATE COMPONENT STOCK
		// UPDATE...JOIN (2 tables) rewritten as scalar subqueries + EXISTS guard
		// (portable MySQL/Postgres). components.status is varchar (literals stay
		// quoted); components.safety_triggered is boolean (literals become
		// true/false). No cross-type cast needed for the merchant_parameters
		// join (both merchant_id columns are varchar).
		innerExpr := round4("? * (SELECT uomc.ratio FROM unit_of_measure_convert uomc WHERE uomc.id_from = c.unit_of_measure AND uomc.id_to = ?)")
		outerExpr := round4(fmt.Sprintf("c.stock - %s", innerExpr))
		_, err := db.ExecContext(ctx, fmt.Sprintf(`
            UPDATE components c
            SET
                stock = %[1]s,
                status = CASE
                    WHEN (SELECT mp.disable_components_under_safety_stock FROM merchant_parameters mp WHERE mp.merchant_id = c.merchant_id)
                         AND (c.stock - %[2]s) < c.safety_stock
                         AND c.safety_triggered = false
                    THEN '0' ELSE c.status END,
                safety_triggered = CASE
                    WHEN (SELECT mp.disable_components_under_safety_stock FROM merchant_parameters mp WHERE mp.merchant_id = c.merchant_id)
                         AND (c.stock - %[2]s) < c.safety_stock
                         AND c.safety_triggered = false
                    THEN true ELSE c.safety_triggered END
            WHERE c.component_id = ?
              AND EXISTS (SELECT 1 FROM unit_of_measure_convert uomc WHERE uomc.id_from = c.unit_of_measure AND uomc.id_to = ?);
        `, outerExpr, innerExpr), req.Qty, req.UOM, req.Qty, req.UOM, req.Qty, req.UOM, req.ObjectID, req.UOM)
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
        `, helpers.GeneratePrefixedID(helpers.StockMovementPrefix), req.ObjectID, req.Qty, req.UOM, req.Comment, userID)
		if err != nil {
			log.Error(err.Error())
			return err
		}

	case "PRODUCT":

		// 1️⃣ GET COMPONENTS IMPACTED BY THE PRODUCT LOSS
		rows, err := db.QueryContext(ctx, fmt.Sprintf(`
            SELECT
                c.merchant_id,
                p.product_id,
                c.component_id,
                %s as qty,
                rq.unit_of_measure
            FROM products p
            INNER JOIN recipes r ON r.product_id = p.product_id
            INNER JOIN requires rq ON rq.recipe_id = r.recipe_id AND rq.enabled IS TRUE
            INNER JOIN components c ON c.component_id = rq.component_id
            INNER JOIN unit_of_measure_convert uomc
                ON uomc.id_from = c.unit_of_measure AND uomc.id_to = rq.unit_of_measure
            WHERE p.product_id = ?;
        `, round4("rq.quantity * uomc.ratio * ?")), req.Qty, req.ObjectID)
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
			_, err = db.ExecContext(ctx, fmt.Sprintf(`
                UPDATE components
                SET stock = %s
                WHERE component_id = ?;
            `, round4("stock - ?")), it.Qty, it.ComponentID)
			if err != nil {
				log.Error(err.Error())
				return err
			}

			_, err = db.ExecContext(ctx, `
                INSERT INTO stock_movements(
                    id, merchant_id, user_id, component_id, product_id, source, movement, quantity, unit_of_measure, order_item_id, comment
                )
                VALUES (?, ?, ?, ?, ?, 'manual', 'loss', ?, ?, NULL, ?);
            `, helpers.GeneratePrefixedID(helpers.StockMovementPrefix), it.MerchantID, userID, it.ComponentID, it.ProductID, it.Qty, it.UOM, req.Comment)
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
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var qCateg, qObjects string

	switch t {
	case "PRODUCT":
		qCateg = `
            SELECT merchant_categ_id, categ_name
            FROM productcateg
            WHERE available = true AND merchant_id = ?
            ORDER BY categ_order ASC;
        `
		qObjects = `
            SELECT product_id, name, category, 1 as unit_of_measure
            FROM products
            WHERE merchant_id = ? AND available = true;
        `

	case "COMPONENT":
		qCateg = `
            SELECT merchant_categ_id, name
            FROM component_category
            WHERE merchant_id = ? AND available = true
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
	db := dbx.GetDB(ctx, r.database)

	idCast := "AS CHAR"
	if dbx.ActiveDialect() == dbx.Postgres {
		idCast = "AS TEXT"
	}

	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			c.component_id,
			c.name,
			COALESCE(CAST(c.unit_of_measure %[1]s), ''),
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
		  AND c.enabled = true
		  AND c.category_id <> 'UBER_EATS_TEMP'
		ORDER BY c.name ASC
	`, idCast), merchantID)
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
	db := dbx.GetDB(ctx, r.database)

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
	// unit_of_measure_convert.id_to is an integer column; MySQL silently
	// coerces a non-numeric req.Unit to 0 (no match -> ErrUnitNotFound),
	// while Postgres raises a hard type error for the same input. Parse
	// up front so both dialects behave identically for garbage client input.
	if _, convErr := strconv.Atoi(req.Unit); convErr != nil {
		return ErrUnitNotFound
	}

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
	// UPDATE...JOIN rewritten as a plain UPDATE (portable MySQL/Postgres) —
	// the ratio was already fetched into a Go value at step 2, so the join
	// was only ever needed to read that same value back inside the UPDATE.
	// Multiplied in Go: `? * ?` between two untyped parameters is ambiguous
	// to Postgres ("operator is not unique").
	convertedQty := req.Quantity * ratio
	if addToStock {
		_, err = db.ExecContext(ctx, fmt.Sprintf(`
			UPDATE components
			SET stock = %s
			WHERE component_id = ? AND merchant_id = ?
		`, round4(fmt.Sprintf("stock + %s", round4("?")))), convertedQty, req.ComponentID, merchantID)
	} else {
		_, err = db.ExecContext(ctx, fmt.Sprintf(`
			UPDATE components
			SET stock = %s
			WHERE component_id = ? AND merchant_id = ?
		`, round4(fmt.Sprintf("stock - %s", round4("?")))), convertedQty, req.ComponentID, merchantID)
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
	`, helpers.GeneratePrefixedID(helpers.StockMovementPrefix), merchantID, userID, req.ComponentID, movementCode, req.Quantity, req.Unit, req.Comment)
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
	db := dbx.GetDB(ctx, r.database)

	// Two quantities per row:
	//   deduct_qty   = rq.quantity * uomc.ratio * oi.quantity  → component native unit (for stock update)
	//   movement_qty = rq.quantity * oi.quantity               → recipe unit (for movement record)
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			oi.order_item_id,
			oi.product_id,
			c.component_id,
			%s AS deduct_qty,
			%s              AS movement_qty,
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
	`, round4("rq.quantity * uomc.ratio * oi.quantity"), round4("rq.quantity * oi.quantity")), merchantID, orderID)
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
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`
			UPDATE components
			SET stock = %s
			WHERE component_id = ? AND merchant_id = ?
		`, round4("stock - ?")), cr.DeductQty, cr.ComponentID, merchantID); err != nil {
			return err
		}

		// Record the movement in the recipe's unit for human-readable traceability.
		if _, err := db.ExecContext(ctx, `
			INSERT INTO stock_movements
				(id, merchant_id, user_id, component_id, product_id, source, movement, quantity, unit_of_measure, order_item_id, order_id)
			VALUES (?, ?, ?, ?, ?, 'order', 'consume', ?, ?, ?, ?)
		`, helpers.GeneratePrefixedID(helpers.StockMovementPrefix), merchantID, userID, cr.ComponentID, cr.ProductID, cr.MovementQty, cr.MovementUOM, cr.OrderItemID, orderID); err != nil {
			return err
		}
	}

	return nil
}

// ConsumeOrderOptionsStock déduit le stock des ingrédients liés aux options
// d'attributs configurables sélectionnées sur la commande (ex. option
// "Extra fromage" liée au composant "Fromage râpé"). Indépendante de
// ConsumeOrderStock (recette du produit) : les deux s'exécutent séparément,
// l'échec de l'une n'affecte jamais l'autre — cf. appelant dans
// order_life_cycle/service.go.
func (r *StocksRepository) ConsumeOrderOptionsStock(ctx context.Context, merchantID, userID, orderID string) error {
	db := dbx.GetDB(ctx, r.database)

	// Même logique à deux quantités que ConsumeOrderStock, au niveau des
	// options sélectionnées plutôt que des composants requis par la recette :
	//   deduct_qty   = cao.quantity * uomc.ratio * oic.quantity → unité native du composant (mise à jour du stock)
	//   movement_qty = cao.quantity * oic.quantity              → unité de l'option (traçabilité du mouvement)
	// Le INNER JOIN sur components exclut naturellement les options sans
	// ingrédient lié (cao.component_id IS NULL ne matche jamais).
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			oi.order_item_id,
			oi.product_id,
			c.component_id,
			%s AS deduct_qty,
			%s              AS movement_qty,
			cao.unit_of_measure                              AS movement_uom
		FROM orderitems oi
		INNER JOIN order_item_configuration oic
			ON oic.order_item_id = oi.order_item_id
		INNER JOIN configurable_attribute_options cao
			ON cao.id = oic.configuration_attribute_option_id
		INNER JOIN components c
			ON c.component_id = cao.component_id AND c.merchant_id = ?
		INNER JOIN unit_of_measure_convert uomc
			ON uomc.id_from = c.unit_of_measure AND uomc.id_to = cao.unit_of_measure
		WHERE oi.order_id = ?
	`, round4("cao.quantity * uomc.ratio * oic.quantity"), round4("cao.quantity * oic.quantity")), merchantID, orderID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type consumptionRow struct {
		OrderItemID string
		ProductID   string
		ComponentID string
		DeductQty   float64 // in component's native unit — used to update components.stock
		MovementQty float64 // in the option's unit — stored in stock_movements
		MovementUOM string  // option's unit id — stored in stock_movements
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
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`
			UPDATE components
			SET stock = %s
			WHERE component_id = ? AND merchant_id = ?
		`, round4("stock - ?")), cr.DeductQty, cr.ComponentID, merchantID); err != nil {
			return err
		}

		// Record the movement in the option's unit for human-readable traceability.
		if _, err := db.ExecContext(ctx, `
			INSERT INTO stock_movements
				(id, merchant_id, user_id, component_id, product_id, source, movement, quantity, unit_of_measure, order_item_id, order_id)
			VALUES (?, ?, ?, ?, ?, 'order', 'consume', ?, ?, ?, ?)
		`, helpers.GeneratePrefixedID(helpers.StockMovementPrefix), merchantID, userID, cr.ComponentID, cr.ProductID, cr.MovementQty, cr.MovementUOM, cr.OrderItemID, orderID); err != nil {
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
	db := dbx.GetDB(ctx, r.database)

	// components.component_id / products.product_id are integer identities
	// while stock_movements.component_id / .product_id are varchar — MySQL
	// implicitly casts across the join and inside COALESCE, Postgres requires
	// explicit casts (and rejects mixing integer/varchar in COALESCE the same
	// way it rejects it in comparisons), and CAST syntax differs per dialect.
	// DATE_FORMAT has no Postgres equivalent (to_char); DATE(x) is valid in
	// MySQL but Postgres uses CAST(x AS date) for the same truncation.
	idCast := "AS CHAR"
	dateExpr := "DATE_FORMAT(sm.movement_date, '%Y-%m-%dT%H:%i:%s')"
	dateTrunc := "DATE(sm.movement_date)"
	if dbx.ActiveDialect() == dbx.Postgres {
		idCast = "AS TEXT"
		dateExpr = `to_char(sm.movement_date, 'YYYY-MM-DD"T"HH24:MI:SS')`
		dateTrunc = "CAST(sm.movement_date AS date)"
	}

	query := fmt.Sprintf(`
		SELECT
			sm.id,
			sm.component_id,
			COALESCE(c.name, sm.component_id)                   AS component_name,
			COALESCE(CAST(c.unit_of_measure %[1]s), sm.unit_of_measure) AS unit_id,
			COALESCE(uomd.uom_desc, sm.unit_of_measure)         AS unit_name,
			COALESCE(uomd.uom_short_desc, sm.unit_of_measure)         AS unit_short_name,
			sm.quantity,
			sm.movement,
			sm.product_id,
			p.name                                               AS product_name,
			%[2]s AS created_at,
			COALESCE(u.name, sm.user_id)                         AS created_by,
			sm.comment
		FROM stock_movements sm
		LEFT JOIN components c
			ON CAST(c.component_id %[1]s) = sm.component_id
		LEFT JOIN unit_of_measure_desc uomd
			ON uomd.id = c.unit_of_measure AND uomd.lang = 'FR'
		LEFT JOIN products p
			ON CAST(p.product_id %[1]s) = sm.product_id
		LEFT JOIN users u
			ON u.user_id = sm.user_id
		WHERE sm.merchant_id = ?
		  AND sm.component_id IS NOT NULL
		  AND %[3]s BETWEEN ? AND ?
		ORDER BY sm.movement_date DESC
	`, idCast, dateExpr, dateTrunc)

	rows, err := db.QueryContext(ctx, query, merchantID, from, to)
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
