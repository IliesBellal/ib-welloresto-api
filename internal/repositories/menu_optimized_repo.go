package repositories

import (
	"context"
	"database/sql"
	"time"

	"welloresto-api/internal/models"
)

type OptimizedMenuRepository struct {
	db *sql.DB
}

func NewOptimizedMenuRepository(db *sql.DB) *OptimizedMenuRepository {
	return &OptimizedMenuRepository{db: db}
}

func (r *OptimizedMenuRepository) GetMenu(ctx context.Context, merchantID string, lastMenu *time.Time) (*models.MenuResponse, error) {

	// TRANSACTION
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	rollback := func() {
		_ = tx.Rollback()
	}

	// -------------------------------------------------------------
	// STEP 1 — check last_menu_update
	// -------------------------------------------------------------
	var dbLast sql.NullTime
	err = tx.QueryRowContext(ctx,
		`SELECT last_menu_update FROM merchant_parameters WHERE merchant_id = ? LIMIT 1`,
		merchantID,
	).Scan(&dbLast)
	if err != nil && err != sql.ErrNoRows {
		rollback()
		return nil, err
	}

	if lastMenu != nil && dbLast.Valid {
		if dbLast.Time.Format("2006-01-02 15:04:05") == lastMenu.Format("2006-01-02 15:04:05") {
			_ = tx.Commit()
			return &models.MenuResponse{
				Status: "no_update_required",
			}, nil
		}
	}

	// -------------------------------------------------------------
	// STEP 2 — categories (1 query)
	// -------------------------------------------------------------
	catRows, err := tx.QueryContext(ctx,
		`SELECT merchant_categ_id, categ_name, categ_order, bg_color
		 FROM productcateg
		 WHERE available = 1 AND enabled = 1 AND merchant_id = ?
		 ORDER BY categ_order ASC`,
		merchantID,
	)
	if err != nil {
		rollback()
		return nil, err
	}
	defer catRows.Close()

	type catTmp struct {
		ID    int64
		Name  string
		Order int
		Bg    sql.NullString
	}
	var categories []catTmp
	for catRows.Next() {
		var c catTmp
		if err := catRows.Scan(&c.ID, &c.Name, &c.Order, &c.Bg); err != nil {
			rollback()
			return nil, err
		}
		categories = append(categories, c)
	}

	// -------------------------------------------------------------
	// STEP 3 — products + subproducts (1 query, LEFT JOIN)
	// -------------------------------------------------------------
	prodRows, err := tx.QueryContext(ctx, `
SELECT 
    p.product_id,
    p.by_product_of,
    p.name,
    p.category,
    p.price,
    p.price_take_away,
    p.price_delivery,
    p.product_desc,
    tva_in.tva_rate AS tva_rate_in,
    tva_delivery.tva_rate AS tva_rate_delivery,
    tva_take.tva_rate AS tva_rate_take_away,
    p.bg_color,
    p.is_product_group,
    p.status,
    p.is_available_on_sno,
    p.is_popular,
    p.image_url,
    p.available_in,
    p.available_take_away,
    p.available_delivery,
    CASE WHEN p.img IS NULL OR p.img='' THEN false ELSE true END AS has_image
FROM products p
INNER JOIN tva_categories tva_in ON tva_in.tva_id = p.tva_in_id
INNER JOIN tva_categories tva_delivery ON tva_delivery.tva_id = p.tva_delivery_id
INNER JOIN tva_categories tva_take ON tva_take.tva_id = p.tva_take_away_id
WHERE p.merchant_id = ? AND p.available = 1 AND p.enabled = 1
ORDER BY p.by_product_of IS NOT NULL, p.category, p.name ASC
`, merchantID)
	if err != nil {
		rollback()
		return nil, err
	}
	defer prodRows.Close()

	products := make(map[int64]*models.ProductEntry)
	subProducts := make([]*models.ProductEntry, 0)

	for prodRows.Next() {
		var p models.ProductEntry

		var by sql.NullInt64
		var desc sql.NullString
		var bg sql.NullString
		var tvaIn, tvaDel, tvaTake sql.NullFloat64
		var imgURL sql.NullString
		var availIn, availTA, availDel sql.NullString
		var isPopular sql.NullBool
		var hasImage bool

		err := prodRows.Scan(
			&p.ProductID,
			&by,
			&p.Name,
			&p.Category,
			&p.Price,
			&p.PriceTakeAway,
			&p.PriceDelivery,
			&desc,
			&tvaIn,
			&tvaDel,
			&tvaTake,
			&bg,
			&p.IsProductGroup,
			&p.Status,
			&p.IsAvailableOnSNO,
			&isPopular,
			&imgURL,
			&availIn,
			&availTA,
			&availDel,
			&hasImage,
		)
		if err != nil {
			rollback()
			return nil, err
		}

		if by.Valid {
			v := by.Int64
			p.ByProductOf = &v
		}
		if desc.Valid {
			p.Description = &desc.String
		}
		if tvaIn.Valid {
			p.TVAIn = &tvaIn.Float64
		}
		if tvaDel.Valid {
			p.TVADelivery = &tvaDel.Float64
		}
		if tvaTake.Valid {
			p.TVATakeAway = &tvaTake.Float64
		}
		if bg.Valid {
			p.BgColor = &bg.String
		}
		if imgURL.Valid {
			p.ImageURL = &imgURL.String
		}
		if availIn.Valid {
			p.AvailableIn = &availIn.String
		}
		if availTA.Valid {
			p.AvailableTakeAway = &availTA.String
		}
		if availDel.Valid {
			p.AvailableDelivery = &availDel.String
		}
		if isPopular.Valid {
			p.IsPopular = isPopular.Bool
		}
		p.HasImage = hasImage

		if p.ByProductOf == nil {
			products[p.ProductID] = &p
		} else {
			subProducts = append(subProducts, &p)
		}
	}

	// -------------------------------------------------------------
	// STEP 4 — attach subproducts to parents
	// -------------------------------------------------------------
	for _, sp := range subProducts {
		parentID := *sp.ByProductOf
		if parent, ok := products[parentID]; ok {
			parent.SubProducts = append(parent.SubProducts, *sp)
		}
	}

	// -------------------------------------------------------------
	// STEP 5 — components (requires) (1 query)
	// -------------------------------------------------------------
	reqRows, err := tx.QueryContext(ctx, `
SELECT r.product_id, c.component_id, c.name, c.component_price, c.status, rq.quantity, uomd.uom_desc
FROM components c
INNER JOIN requires rq ON rq.component_id = c.component_id AND rq.enabled = true
INNER JOIN recipes r ON r.recipe_id = rq.recipe_id
INNER JOIN unit_of_measure_desc uomd ON uomd.id = rq.unit_of_measure AND uomd.lang='FR'
WHERE c.merchant_id = ? AND c.available = 1
`, merchantID)
	if err != nil {
		rollback()
		return nil, err
	}
	defer reqRows.Close()

	compMap := make(map[int64][]models.ComponentUsage)
	for reqRows.Next() {
		var pid int64
		var cu models.ComponentUsage
		var uom sql.NullString

		err := reqRows.Scan(
			&pid,
			&cu.ComponentID,
			&cu.Name,
			&cu.Price,
			&cu.Status,
			&cu.Quantity,
			&uom,
		)
		if err != nil {
			rollback()
			return nil, err
		}
		if uom.Valid {
			cu.UnitOfMeasure = uom.String
		}
		compMap[pid] = append(compMap[pid], cu)
	}

	for pid, comps := range compMap {
		if p := products[pid]; p != nil {
			p.Components = comps
		}
	}

	// -------------------------------------------------------------
	// STEP 6 — configurable attributes + options (JOIN + GROUP BY)
	// -------------------------------------------------------------
	attrRows, err := tx.QueryContext(ctx, `
SELECT 
    ca.id,
    pca.product_id,
    ca.title,
    ca.max_options,
    ca.min_options,
    ca.attribute_type,
    cao.id,
    cao.title,
    cao.extra_price,
    cao.max_quantity
FROM product_configurable_attribute pca
INNER JOIN configurable_attributes ca ON ca.id = pca.configurable_attribute_id
LEFT JOIN configurable_attribute_options cao ON cao.configurable_attribute_id = ca.id AND cao.enabled = 1
INNER JOIN products p ON p.product_id = pca.product_id
WHERE p.merchant_id = ? AND ca.enabled = 1 AND pca.enabled = 1
ORDER BY pca.product_id, pca.num_order ASC
`, merchantID)
	if err != nil {
		rollback()
		return nil, err
	}
	defer attrRows.Close()

	type tempAttr struct {
		Attr models.ConfigurableAttribute
	}
	tmp := make(map[int64]map[int64]*models.ConfigurableAttribute)

	for attrRows.Next() {
		var (
			attrID    int64
			productID int64
			title     string
			maxOps    int
			minOps    int
			attrType  string
			optID     sql.NullInt64
			optTitle  sql.NullString
			optPrice  sql.NullInt64
			optMaxQty sql.NullInt64
		)

		err := attrRows.Scan(
			&attrID, &productID, &title, &maxOps, &minOps, &attrType,
			&optID, &optTitle, &optPrice, &optMaxQty,
		)
		if err != nil {
			rollback()
			return nil, err
		}

		if tmp[productID] == nil {
			tmp[productID] = make(map[int64]*models.ConfigurableAttribute)
		}
		if _, exists := tmp[productID][attrID]; !exists {
			tmp[productID][attrID] = &models.ConfigurableAttribute{
				ID:            attrID,
				ProductID:     productID,
				Title:         title,
				MaxOptions:    maxOps,
				MinOptions:    minOps,
				AttributeType: attrType,
				Options:       []models.ConfigurableOption{},
			}
		}

		if optID.Valid {
			tmp[productID][attrID].Options = append(tmp[productID][attrID].Options, models.ConfigurableOption{
				ID:          optID.Int64,
				Title:       optTitle.String,
				ExtraPrice:  int(optPrice.Int64),
				MaxQuantity: int(optMaxQty.Int64),
			})
		}
	}

	// attach attributes to products
	for pid, attrs := range tmp {
		if p := products[pid]; p != nil {
			for _, a := range attrs {
				p.Configuration.Attributes = append(p.Configuration.Attributes, *a)
			}
		}
	}

	// -------------------------------------------------------------
	// STEP 7 — delays
	// -------------------------------------------------------------
	delayRows, err := tx.QueryContext(ctx,
		`SELECT id, short_description, duration 
         FROM delays 
         WHERE enabled = true 
         ORDER BY duration ASC`)
	if err != nil {
		rollback()
		return nil, err
	}
	defer delayRows.Close()

	var delays []models.DelayEntry
	for delayRows.Next() {
		var d models.DelayEntry
		if err := delayRows.Scan(&d.DelayID, &d.ShortDescription, &d.Duration); err != nil {
			rollback()
			return nil, err
		}
		delays = append(delays, d)
	}

	// -------------------------------------------------------------
	// STEP 8 — component categories + global components
	// -------------------------------------------------------------
	compCatRows, err := tx.QueryContext(ctx,
		`SELECT merchant_categ_id, name, categ_order 
         FROM component_category
         WHERE merchant_id = ? AND available = 1
         ORDER BY categ_order ASC`, merchantID)
	if err != nil {
		rollback()
		return nil, err
	}
	defer compCatRows.Close()

	type compCatTmp struct {
		ID    int64
		Name  string
		Order int
	}
	var compCats []compCatTmp
	for compCatRows.Next() {
		var x compCatTmp
		if err := compCatRows.Scan(&x.ID, &x.Name, &x.Order); err != nil {
			rollback()
			return nil, err
		}
		compCats = append(compCats, x)
	}

	allCompRows, err := tx.QueryContext(ctx,
		`SELECT component_id, name, category_id, status, component_price
		 FROM components
		 WHERE merchant_id = ?`, merchantID)
	if err != nil {
		rollback()
		return nil, err
	}
	defer allCompRows.Close()

	type compBasicTmp struct {
		ID     int64
		Name   string
		CatID  int64
		Status int
		Price  int
	}
	var allComps []compBasicTmp
	for allCompRows.Next() {
		var c compBasicTmp
		err := allCompRows.Scan(&c.ID, &c.Name, &c.CatID, &c.Status, &c.Price)
		if err != nil {
			rollback()
			return nil, err
		}
		allComps = append(allComps, c)
	}

	var compTypes []models.ComponentCategory
	for _, cat := range compCats {
		var items []models.ComponentBasic
		for _, c := range allComps {
			if c.CatID == cat.ID {
				items = append(items, models.ComponentBasic{
					ComponentID: c.ID,
					Name:        c.Name,
					Category:    c.CatID,
					Price:       c.Price,
					Status:      c.Status,
				})
			}
		}
		compTypes = append(compTypes, models.ComponentCategory{
			Category:   cat.Name,
			Order:      cat.Order,
			Components: items,
		})
	}

	// -------------------------------------------------------------
	// BUILD products_types
	// -------------------------------------------------------------
	var productTypes []models.ProductCategory

	for _, c := range categories {
		var list []models.ProductEntry

		// keep order stable: category then alphabetical name
		for _, p := range products {
			if p.Category == c.ID {
				list = append(list, *p)
			}
		}

		var bg *string
		if c.Bg.Valid {
			bg = &c.Bg.String
		}

		productTypes = append(productTypes, models.ProductCategory{
			Category:   c.Name,
			CategoryID: c.ID,
			Order:      c.Order,
			BgColor:    bg,
			Products:   list,
		})
	}

	// -------------------------------------------------------------
	// COMMIT
	// -------------------------------------------------------------
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	var lastMenuResp *time.Time
	if dbLast.Valid {
		t := dbLast.Time
		lastMenuResp = &t
	}

	return &models.MenuResponse{
		Status:          "ok",
		LastMenuUpdate:  lastMenuResp,
		ProductsTypes:   productTypes,
		ComponentsTypes: compTypes,
		Delays:          delays,
	}, nil
}
