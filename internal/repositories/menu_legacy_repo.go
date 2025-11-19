package repositories

import (
	"context"
	"database/sql"
	"time"
	"welloresto-api/internal/models"
)

type LegacyMenuRepository struct {
	db *sql.DB
}

func NewLegacyMenuRepository(db *sql.DB) *LegacyMenuRepository {
	return &LegacyMenuRepository{db: db}
}

func (r *LegacyMenuRepository) GetMenu(ctx context.Context, merchantID string, lastMenu *time.Time) (*models.MenuResponse, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		// caller will get commit/rollback inside; ensure rollback on panic
	}()

	// Step 1 : get last_menu_update
	var dbLastMenu sql.NullTime
	err = tx.QueryRowContext(ctx, "SELECT last_menu_update FROM merchant_parameters WHERE merchant_id = ? LIMIT 1", merchantID).Scan(&dbLastMenu)
	if err != nil && err != sql.ErrNoRows {
		tx.Rollback()
		return nil, err
	}

	// if lastMenu provided and equals DB => no_update_required
	if lastMenu != nil && dbLastMenu.Valid {
		if dbLastMenu.Time.Format("2006-01-02 15:04:05") == lastMenu.Format("2006-01-02 15:04:05") {
			tx.Commit()
			return &models.MenuResponse{Status: "no_update_required"}, nil
		}
	}

	// perform queries exactly as PHP: categories, products, subproducts, components, comp categories, all components, delays, configurable attributes, options
	// 1) categories
	catRows, err := tx.QueryContext(ctx, `
		SELECT pc.merchant_categ_id, pc.categ_name, pc.categ_order, pc.bg_color
		FROM productcateg pc
		WHERE pc.available = 1 AND pc.enabled = 1 AND pc.merchant_id = ?
		ORDER BY pc.categ_order ASC
	`, merchantID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	defer catRows.Close()

	type catTmp struct {
		ID    *string
		Name  string
		Order int
		Bg    sql.NullString
	}
	var cats []catTmp
	for catRows.Next() {
		var c catTmp
		if err := catRows.Scan(&c.ID, &c.Name, &c.Order, &c.Bg); err != nil {
			tx.Rollback()
			return nil, err
		}
		cats = append(cats, c)
	}

	// 2) products (roots)
	prodRows, err := tx.QueryContext(ctx, `
	SELECT p.product_id, p.by_product_of, p.name, p.category, p.price, p.price_take_away, p.price_delivery, p.product_desc, 
	       tva_in.tva_rate as tva_rate_in, tva_delivery.tva_rate as tva_rate_delivery, tva_take_away.tva_rate as tva_rate_take_away, 
	       p.bg_color, p.is_product_group, p.status, p.is_available_on_sno, p.is_popular, p.image_url, p.available_in, p.available_take_away, p.available_delivery,
	       CASE WHEN p.img IS NULL OR p.img = '' THEN false ELSE true END as has_image
	FROM products p
	INNER JOIN tva_categories tva_in on tva_in.tva_id = p.tva_in_id
	INNER JOIN tva_categories tva_delivery on tva_delivery.tva_id = p.tva_delivery_id
	INNER JOIN tva_categories tva_take_away on tva_take_away.tva_id = p.tva_take_away_id
	LEFT JOIN products subp on subp.product_id = p.by_product_of
	WHERE p.merchant_id = ? AND (subp.product_id IS NULL OR subp.product_id = p.product_id) AND p.available = 1 AND p.enabled = 1
	`, merchantID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	defer prodRows.Close()

	// map productID -> ProductEntry
	products := make(map[string]*models.ProductEntry)
	var productOrder []string
	for prodRows.Next() {
		var p models.ProductEntry
		var tvaIn, tvaDel, tvaTake sql.NullFloat64
		var bg sql.NullString
		var hasImage bool
		var desc sql.NullString
		var imageURL sql.NullString
		var availIn, availTake, availDel sql.NullBool
		var isPopular sql.NullBool

		if err := prodRows.Scan(&p.ProductID, &p.ByProductOf, &p.Name, &p.Category, &p.Price, &p.PriceTakeAway, &p.PriceDelivery,
			&desc, &tvaIn, &tvaDel, &tvaTake, &bg, &p.IsProductGroup, &p.Status, &p.IsAvailableOnSNO, &isPopular, &imageURL,
			&availIn, &availTake, &availDel, &hasImage); err != nil {
			tx.Rollback()
			return nil, err
		}
		if tvaIn.Valid {
			p.TVAIn = tvaIn.Float64
		}
		if tvaDel.Valid {
			p.TVADelivery = tvaDel.Float64
		}
		if tvaTake.Valid {
			p.TVATakeAway = tvaTake.Float64
		}
		if bg.Valid {
			p.BgColor = &bg.String
		}
		if desc.Valid {
			p.Description = &desc.String
		}
		if imageURL.Valid {
			p.ImageURL = &imageURL.String
		}
		if isPopular.Valid {
			p.IsPopular = isPopular.Bool
		}
		if availIn.Valid {
			p.AvailableIn = availIn.Bool
		}
		if availTake.Valid {
			p.AvailableTakeAway = availTake.Bool
		}
		if availDel.Valid {
			p.AvailableDelivery = availDel.Bool
		}

		products[p.ProductID] = &p
		productOrder = append(productOrder, p.ProductID)
	}

	// 3) sub-products
	subRows, err := tx.QueryContext(ctx, `
	SELECT p.product_id, p.by_product_of, p.name, p.category, p.price, p.price_take_away, p.price_delivery, p.product_desc,
	       tva_in.tva_rate as tva_rate_in, tva_delivery.tva_rate as tva_rate_delivery, tva_take_away.tva_rate as tva_rate_take_away, p.bg_color, p.is_product_group, p.is_available_on_sno, p.status
	FROM products p
	INNER JOIN tva_categories tva_in on tva_in.tva_id = p.tva_in_id
	INNER JOIN tva_categories tva_delivery on tva_delivery.tva_id = p.tva_delivery_id
	INNER JOIN tva_categories tva_take_away on tva_take_away.tva_id = p.tva_take_away_id
	WHERE p.merchant_id = ? AND p.by_product_of IS NOT NULL AND p.available = 1
	`, merchantID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	defer subRows.Close()

	subProducts := make(map[string]*models.ProductEntry)
	for subRows.Next() {
		var p models.ProductEntry
		var by sql.NullString
		var tvaIn, tvaDel, tvaTake sql.NullFloat64
		var bg sql.NullString
		var desc sql.NullString
		if err := subRows.Scan(&p.ProductID, &by, &p.Name, &p.Category, &p.Price, &p.PriceTakeAway, &p.PriceDelivery, &desc, &tvaIn, &tvaDel, &tvaTake, &bg, &p.IsProductGroup, &p.IsAvailableOnSNO, &p.Status); err != nil {
			tx.Rollback()
			return nil, err
		}
		if by.Valid {
			p.ByProductOf = &by.String
		}
		if tvaIn.Valid {
			p.TVAIn = tvaIn.Float64
		}
		if tvaDel.Valid {
			p.TVADelivery = tvaDel.Float64
		}
		if tvaTake.Valid {
			p.TVATakeAway = tvaTake.Float64
		}
		if bg.Valid {
			p.BgColor = &bg.String
		}
		if desc.Valid {
			p.Description = &desc.String
		}
		subProducts[p.ProductID] = &p
	}

	// 4) components (requires + components + recipes -> map product_id -> []ComponentUsage)
	compRows, err := tx.QueryContext(ctx, `
	SELECT r.product_id, c.component_id, c.name, c.component_price, c.status, rq.quantity, uomd.uom_desc
	FROM components c
	INNER JOIN requires rq on c.component_id = rq.component_id and rq.enabled = true
	INNER JOIN recipes r on r.recipe_id = rq.recipe_id
	INNER JOIN unit_of_measure_desc uomd on uomd.lang = 'FR' and uomd.id = rq.unit_of_measure
	WHERE c.merchant_id = ? AND c.available = 1 AND rq.enabled = true
	`, merchantID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	defer compRows.Close()

	type compTmp struct {
		ProductID int64
		Comp      models.ComponentUsage
	}
	compMap := make(map[string][]models.ComponentUsage)
	for compRows.Next() {
		var productID string
		var c models.ComponentUsage
		var uom sql.NullString
		if err := compRows.Scan(&productID, &c.ComponentID, &c.Name, &c.Price, &c.Status, &c.Quantity, &uom); err != nil {
			tx.Rollback()
			return nil, err
		}
		if uom.Valid {
			c.UnitOfMeasure = uom.String
		}
		compMap[productID] = append(compMap[productID], c)
	}

	// 5) configurable attributes (and options) - build map productID -> attributes
	attrRows, err := tx.QueryContext(ctx, `
		SELECT ca.id, pca.product_id, ca.title, ca.max_options, ca.attribute_type, ca.min_options
		FROM products p
		INNER JOIN product_configurable_attribute pca on pca.product_id = p.product_id
		INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
		WHERE p.merchant_id = ? AND ca.enabled = 1 AND pca.enabled = 1
		ORDER BY pca.num_order ASC
	`, merchantID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	defer attrRows.Close()

	// load options (distinct)
	optRows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT ca.id as configurable_attribute_id, cao.id, cao.title, cao.extra_price, cao.max_quantity
		FROM products p
		INNER JOIN product_configurable_attribute pca on pca.product_id = p.product_id
		INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
		INNER JOIN configurable_attribute_options cao on cao.configurable_attribute_id = ca.id
		WHERE p.merchant_id = ? AND ca.enabled = 1 AND cao.enabled = 1 AND pca.enabled = 1
	`, merchantID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	defer optRows.Close()

	optMap := make(map[string][]models.ConfigurableOption)
	for optRows.Next() {
		var cfgID string
		var o models.ConfigurableOption
		if err := optRows.Scan(&cfgID, &o.ID, &o.Title, &o.ExtraPrice, &o.MaxQuantity); err != nil {
			tx.Rollback()
			return nil, err
		}
		optMap[cfgID] = append(optMap[cfgID], o)
	}

	attrMap := make(map[string][]models.ConfigurableAttribute)
	for attrRows.Next() {
		var a models.ConfigurableAttribute
		if err := attrRows.Scan(&a.ID, &a.ProductID, &a.Title, &a.MaxOptions, &a.AttributeType, &a.MinOptions); err != nil {
			tx.Rollback()
			return nil, err
		}
		// attach options
		a.Options = optMap[a.ID]
		attrMap[a.ProductID] = append(attrMap[a.ProductID], a)
	}

	// 6) delays
	delayRows, err := tx.QueryContext(ctx, `SELECT id, short_description, duration FROM delays WHERE enabled = true ORDER BY duration ASC`)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	defer delayRows.Close()
	var delays []models.DelayEntry
	for delayRows.Next() {
		var d models.DelayEntry
		if err := delayRows.Scan(&d.DelayID, &d.ShortDescription, &d.Duration); err != nil {
			tx.Rollback()
			return nil, err
		}
		delays = append(delays, d)
	}

	// 7) components categories and all components
	compCatRows, err := tx.QueryContext(ctx, `SELECT merchant_categ_id, name, categ_order FROM component_category WHERE merchant_id = ? AND available = 1 ORDER BY categ_order ASC`, merchantID)
	if err != nil {
		tx.Rollback()
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
		var c compCatTmp
		if err := compCatRows.Scan(&c.ID, &c.Name, &c.Order); err != nil {
			tx.Rollback()
			return nil, err
		}
		compCats = append(compCats, c)
	}

	allCompRows, err := tx.QueryContext(ctx, `SELECT component_id, name, category_id, status, component_price FROM components WHERE merchant_id = ?`, merchantID)
	if err != nil {
		tx.Rollback()
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
	var allComponents []compBasicTmp
	for allCompRows.Next() {
		var cb compBasicTmp
		if err := allCompRows.Scan(&cb.ID, &cb.Name, &cb.CatID, &cb.Status, &cb.Price); err != nil {
			tx.Rollback()
			return nil, err
		}
		allComponents = append(allComponents, cb)
	}

	// Build product list: attach components, subproducts, configuration
	// first attach subproducts to parents
	for _, sp := range subProducts {
		if sp.ByProductOf != nil {
			parent := products[*sp.ByProductOf]
			if parent != nil {
				parent.SubProducts = append(parent.SubProducts, *sp)
			}
		}
	}

	// attach components & configuration for each product
	for _, p := range products {
		if comps, ok := compMap[p.ProductID]; ok {
			p.Components = comps
		}
		if attrs, ok := attrMap[p.ProductID]; ok {
			p.Configuration = models.ConfigurableResponse{Attributes: attrs}
		} else {
			p.Configuration = models.ConfigurableResponse{Attributes: []models.ConfigurableAttribute{}}
		}
	}

	// build categories -> products mapping preserving categ_order and internal product ordering by product_id (you said you'll add product_order later)
	var productTypes []models.ProductCategory
	for _, c := range cats {
		var actual []models.ProductEntry
		for _, pid := range productOrder {
			p := products[pid]
			if p != nil && p.Category == c.ID {
				actual = append(actual, *p)
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
			Products:   actual,
		})
	}

	// build component types
	var compTypes []models.ComponentCategory
	for _, cc := range compCats {
		var actual []models.ComponentBasic
		for _, cb := range allComponents {
			if cb.CatID == cc.ID {
				actual = append(actual, models.ComponentBasic{
					ComponentID: cb.ID,
					Name:        cb.Name,
					Category:    cb.CatID,
					Price:       cb.Price,
					Status:      cb.Status,
				})
			}
		}
		compTypes = append(compTypes, models.ComponentCategory{
			Category: cc.Name, Order: cc.Order, Components: actual,
		})
	}

	// commit and return
	tx.Commit()

	var lastMenuTime *time.Time
	if dbLastMenu.Valid {
		t := dbLastMenu.Time
		lastMenuTime = &t
	}

	resp := &models.MenuResponse{
		Status:          "ok",
		LastMenuUpdate:  lastMenuTime,
		ProductsTypes:   productTypes,
		ComponentsTypes: compTypes,
		Delays:          delays,
	}
	return resp, nil
}
