package stocks

import (
	"context"
	"database/sql"
	"errors"
	"time"
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
	// VALUES(:merchant_id, :user_id, :component_id, '2', '1',:c_quantity*:bc_quantity, :bc_uom, UTC_TIMESTAMP)
	_, err = db.ExecContext(ctx, `
		INSERT INTO stock_movements(merchant_id, user_id, component_id, source, movement, quantity, unit_of_measure, movement_date)
		VALUES (?, ?, ?, '2', '1', ? * ?, ?, UTC_TIMESTAMP)
	`, merchantID, userID, s.ComponentID, s.CQuantity, s.BCQuantity, s.BCUOM)
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
			INSERT INTO expiration_dates(merchant_id, component_id, expiration_date, creation_date, purchased_component_id)
			VALUES (?, ?, ?, UTC_TIMESTAMP, ?)
		`, merchantID, s.ComponentID, *s.DLC, purchasedComponentID)
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
                (merchant_id, user_id, component_id, product_id, source, movement, quantity, unit_of_measure, order_item_id, comment)
            SELECT u.merchant_id, u.user_id, ?, NULL, 2, 4, ?, ?, NULL, ?
            FROM users u
            WHERE u.user_id = ?;
        `, req.ObjectID, req.Qty, req.UOM, req.Comment, userID)
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
                    merchant_id, user_id, component_id, product_id, source, movement, quantity, unit_of_measure, order_item_id, comment
                )
                VALUES (?, ?, ?, ?, 2, 4, ?, ?, NULL, ?);
            `, it.MerchantID, userID, it.ComponentID, it.ProductID, it.Qty, it.UOM, req.Comment)
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
            WHERE merchant_id = ?;
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
