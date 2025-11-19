package repositories

import (
	"context"
	"database/sql"
	"fmt"
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

	// --------------------------
	// 1) get last_menu_update
	// --------------------------
	var dbLastMenu sql.NullTime
	err := r.db.QueryRowContext(ctx,
		"SELECT last_menu_update FROM merchant_parameters WHERE merchant_id = ? LIMIT 1",
		merchantID,
	).Scan(&dbLastMenu)

	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("QueryRowContext last menu: %w", err)
	}

	// If client has same timestamp -> no update required
	if lastMenu != nil && dbLastMenu.Valid {
		if dbLastMenu.Time.Format("2006-01-02 15:04:05") ==
			lastMenu.Format("2006-01-02 15:04:05") {

			return &models.MenuResponse{
				Status: "no_update_required",
			}, nil
		}
	}

	// ------------------------------------------------------------------
	// 2) categories
	// ------------------------------------------------------------------
	catRows, err := r.db.QueryContext(ctx, `
		SELECT pc.merchant_categ_id, pc.categ_name, pc.categ_order, pc.bg_color
		FROM productcateg pc
		WHERE pc.available = 1 AND pc.enabled = 1 AND pc.merchant_id = ?
		ORDER BY pc.categ_order ASC
	`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("categories query: %w", err)
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
			return nil, fmt.Errorf("categories scan: %w", err)
		}
		cats = append(cats, c)
	}

	// ------------------------------------------------------------------
	// 3) products (roots)
	// ------------------------------------------------------------------
	prodRows, err := r.db.QueryContext(ctx, `
	SELECT p.product_id, p.by_product_of, p.name, p.category, p.price,
	       p.price_take_away, p.price_delivery, p.product_desc,
	       tva_in.tva_rate, tva_delivery.tva_rate, tva_take_away.tva_rate,
	       p.bg_color, p.is_product_group, p.status, p.is_available_on_sno,
	       p.is_popular, p.image_url,
	       p.available_in, p.available_take_away, p.available_delivery,
	       CASE WHEN p.img IS NULL OR p.img = '' THEN false ELSE true END as has_image
	FROM products p
	INNER JOIN tva_categories tva_in        ON tva_in.tva_id = p.tva_in_id
	INNER JOIN tva_categories tva_delivery  ON tva_delivery.tva_id = p.tva_delivery_id
	INNER JOIN tva_categories tva_take_away ON tva_take_away.tva_id = p.tva_take_away_id
	LEFT JOIN products subp ON subp.product_id = p.by_product_of
	WHERE p.merchant_id = ?
	  AND (subp.product_id IS NULL OR subp.product_id = p.product_id)
	  AND p.available = 1 AND p.enabled = 1
	`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("products query: %w", err)
	}
	defer prodRows.Close()

	products := make(map[string]*models.ProductEntry)
	var productOrder []string

	for prodRows.Next() {
		var p models.ProductEntry
		var by sql.NullString
		var desc sql.NullString
		var bg sql.NullString
		var tvaIn, tvaDel, tvaTake sql.NullFloat64
		var imgURL sql.NullString
		var availIn, availTake, availDel sql.NullBool
		var isPop sql.NullBool
		var hasImg bool

		err := prodRows.Scan(
			&p.ProductID, &by, &p.Name, &p.Category, &p.Price,
			&p.PriceTakeAway, &p.PriceDelivery, &desc,
			&tvaIn, &tvaDel, &tvaTake,
			&bg, &p.IsProductGroup, &p.Status, &p.IsAvailableOnSNO,
			&isPop, &imgURL,
			&availIn, &availTake, &availDel,
			&hasImg,
		)
		if err != nil {
			return nil, fmt.Errorf("products scan: %w", err)
		}

		if by.Valid {
			p.ByProductOf = &by.String
		}
		if desc.Valid {
			p.Description = &desc.String
		}
		if bg.Valid {
			p.BgColor = &bg.String
		}
		if imgURL.Valid {
			p.ImageURL = &imgURL.String
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
		if availIn.Valid {
			p.AvailableIn = availIn.Bool
		}
		if availTake.Valid {
			p.AvailableTakeAway = availTake.Bool
		}
		if availDel.Valid {
			p.AvailableDelivery = availDel.Bool
		}
		if isPop.Valid {
			p.IsPopular = isPop.Bool
		}

		p.HasImage = hasImg

		products[p.ProductID] = &p
		productOrder = append(productOrder, p.ProductID)
	}

	// ------------------------------------------------------------------
	// 4) subproducts
	// ------------------------------------------------------------------
	subRows, err := r.db.QueryContext(ctx, `
	SELECT p.product_id, p.by_product_of, p.name, p.category, p.price,
	       p.price_take_away, p.price_delivery, p.product_desc,
	       tva_in.tva_rate, tva_delivery.tva_rate, tva_take_away.tva_rate,
	       p.bg_color, p.is_product_group, p.is_available_on_sno, p.status
	FROM products p
	INNER JOIN tva_categories tva_in        ON tva_in.tva_id = p.tva_in_id
	INNER JOIN tva_categories tva_delivery  ON tva_delivery.tva_id = p.tva_delivery_id
	INNER JOIN tva_categories tva_take_away ON tva_take_away.tva_id = p.tva_take_away_id
	WHERE p.merchant_id = ?
	  AND p.by_product_of IS NOT NULL
	  AND p.available = 1
	`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("subproducts query: %w", err)
	}
	defer subRows.Close()

	subProducts := make(map[string]*models.ProductEntry)

	for subRows.Next() {
		var p models.ProductEntry
		var by sql.NullString
		var desc sql.NullString
		var bg sql.NullString
		var tvaIn, tvaDel, tvaTake sql.NullFloat64

		err := subRows.Scan(
			&p.ProductID, &by, &p.Name, &p.Category, &p.Price,
			&p.PriceTakeAway, &p.PriceDelivery, &desc,
			&tvaIn, &tvaDel, &tvaTake,
			&bg, &p.IsProductGroup, &p.IsAvailableOnSNO, &p.Status,
		)
		if err != nil {
			return nil, fmt.Errorf("subproducts scan: %w", err)
		}

		if by.Valid {
			p.ByProductOf = &by.String
		}
		if desc.Valid {
			p.Description = &desc.String
		}
		if bg.Valid {
			p.BgColor = &bg.String
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

		subProducts[p.ProductID] = &p
	}

	// ------------------------------------------------------------------
	// 5) components (requires)
	// ------------------------------------------------------------------
	compRows, err := r.db.QueryContext(ctx, `
	SELECT r.product_id, c.component_id, c.name, c.component_price,
	       c.status, rq.quantity, uomd.uom_desc
	FROM components c
	INNER JOIN requires rq ON c.component_id = rq.component_id AND rq.enabled = TRUE
	INNER JOIN recipes rON r.recipe_id = rq.recipe_id
	INNER JOIN unit_of_measure_desc uomd ON uomd.lang = 'FR' AND uomd.id = rq.unit_of_measure
	WHERE c.merchant_id = ? AND c.available = 1
	`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("components query: %w", err)
	}
	defer compRows.Close()

	compMap := make(map[string][]models.ComponentUsage)

	for compRows.Next() {
		var pid string
		var c models.ComponentUsage
		var uom sql.NullString

		err := compRows.Scan(
			&pid, &c.ComponentID, &c.Name, &c.Price,
			&c.Status, &c.Quantity, &uom,
		)
		if err != nil {
			return nil, fmt.Errorf("components scan: %w", err)
		}
		if uom.Valid {
			c.UnitOfMeasure = uom.String
		}
		compMap[pid] = append(compMap[pid], c)
	}

	// ------------------------------------------------------------------
	// 6) configurable attributes + options
	// ------------------------------------------------------------------
	attrRows, err := r.db.QueryContext(ctx, `
		SELECT ca.id, pca.product_id, ca.title, ca.max_options,
		       ca.attribute_type, ca.min_options
		FROM products p
		INNER JOIN product_configurable_attribute pca
		    ON p.product_id = pca.product_id
		INNER JOIN configurable_attributes ca
		    ON ca.id = pca.configurable_attribute_id
		WHERE p.merchant_id = ?
		  AND ca.enabled = 1
		  AND pca.enabled = 1
		ORDER BY pca.num_order ASC
	`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("attributes query: %w", err)
	}
	defer attrRows.Close()

	optRows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT ca.id, cao.id, cao.title, cao.extra_price, cao.max_quantity
		FROM products p
		INNER JOIN product_configurable_attribute pca
		    ON p.product_id = pca.product_id
		INNER JOIN configurable_attributes ca
		    ON ca.id = pca.configurable_attribute_id
		INNER JOIN configurable_attribute_options cao
		    ON ca.id = cao.configurable_attribute_id
		WHERE p.merchant_id = ?
		  AND ca.enabled = 1
		  AND cao.enabled = 1
		  AND pca.enabled = 1
	`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("options query: %w", err)
	}
	defer optRows.Close()

	optMap := make(map[string][]models.ConfigurableOption)

	for optRows.Next() {
		var cfgID string
		var opt models.ConfigurableOption

		err := optRows.Scan(
			&cfgID, &opt.ID, &opt.Title, &opt.ExtraPrice, &opt.MaxQuantity,
		)
		if err != nil {
			return nil, fmt.Errorf("options scan: %w", err)
		}

		optMap[cfgID] = append(optMap[cfgID], opt)
	}

	attrMap := make(map[string][]models.ConfigurableAttribute)

	for attrRows.Next() {
		var a models.ConfigurableAttribute

		err := attrRows.Scan(
			&a.ID, &a.ProductID, &a.Title,
			&a.MaxOptions, &a.AttributeType, &a.MinOptions,
		)
		if err != nil {
			return nil, fmt.Errorf("attributes scan: %w", err)
		}

		a.Options = optMap[a.ID]
		attrMap[a.ProductID] = append(attrMap[a.ProductID], a)
	}

	// ------------------------------------------------------------------
	// 7) delays
	// ------------------------------------------------------------------
	delayRows, err := r.db.QueryContext(ctx, `
		SELECT id, short_description, duration
		FROM delays
		WHERE enabled = TRUE
		ORDER BY duration ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("delays query: %w", err)
	}
	defer delayRows.Close()

	var delays []models.DelayEntry

	for delayRows.Next() {
		var d models.DelayEntry
		if err := delayRows.Scan(&d.DelayID, &d.ShortDescription, &d.Duration); err != nil {
			return nil, fmt.Errorf("delays scan: %w", err)
		}
		delays = append(delays, d)
	}

	// ------------------------------------------------------------------
	// 8) component categories + all components
	// ------------------------------------------------------------------
	catCompRows, err := r.db.QueryContext(ctx, `
		SELECT merchant_categ_id, name, categ_order
		FROM component_category
		WHERE merchant_id = ? AND available = 1
		ORDER BY categ_order ASC
	`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("component categories query: %w", err)
	}
	defer catCompRows.Close()

	type compCatTmp struct {
		ID    int64
		Name  string
		Order int
	}
	var compCats []compCatTmp

	for catCompRows.Next() {
		var c compCatTmp
		if err := catCompRows.Scan(&c.ID, &c.Name, &c.Order); err != nil {
			return nil, fmt.Errorf("component categories scan: %w", err)
		}
		compCats = append(compCats, c)
	}

	allCompRows, err := r.db.QueryContext(ctx, `
		SELECT component_id, name, category_id, status, component_price
		FROM components
		WHERE merchant_id = ?
	`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("all components query: %w", err)
	}
	defer allCompRows.Close()

	type compBasicTmp struct {
		ID     int64
		Name   string
		CatID  int64
		Price  int
		Status int
	}

	var allComponents []compBasicTmp

	for allCompRows.Next() {
		var c compBasicTmp
		err := allCompRows.Scan(&c.ID, &c.Name, &c.CatID, &c.Status, &c.Price)
		if err != nil {
			return nil, fmt.Errorf("all components scan: %w", err)
		}
		allComponents = append(allComponents, c)
	}

	// ------------------------------------------------------------------
	// 9) attach subproducts + components + attributes
	// ------------------------------------------------------------------

	// attach subproducts to parent
	for _, sp := range subProducts {
		if sp.ByProductOf != nil {
			parent := products[*sp.ByProductOf]
			if parent != nil {
				parent.SubProducts = append(parent.SubProducts, *sp)
			}
		}
	}

	// attach components + attributes
	for _, p := range products {
		if arr, ok := compMap[p.ProductID]; ok {
			p.Components = arr
		}
		if attrs, ok := attrMap[p.ProductID]; ok {
			p.Configuration = models.ConfigurableResponse{Attributes: attrs}
		} else {
			p.Configuration = models.ConfigurableResponse{Attributes: []models.ConfigurableAttribute{}}
		}
	}

	// ------------------------------------------------------------------
	// 10) Build productTypes
	// ------------------------------------------------------------------
	var productTypes []models.ProductCategory

	for _, c := range cats {
		var productsInCat []models.ProductEntry

		for _, pid := range productOrder {
			p := products[pid]
			if p != nil && p.Category == c.ID {
				productsInCat = append(productsInCat, *p)
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
			Products:   productsInCat,
		})
	}

	// ------------------------------------------------------------------
	// 11) Build componentTypes
	// ------------------------------------------------------------------
	var compTypes []models.ComponentCategory

	for _, cc := range compCats {
		var arr []models.ComponentBasic

		for _, c := range allComponents {
			if c.CatID == cc.ID {
				arr = append(arr, models.ComponentBasic{
					ComponentID: c.ID,
					Name:        c.Name,
					Category:    c.CatID,
					Price:       c.Price,
					Status:      c.Status,
				})
			}
		}

		compTypes = append(compTypes, models.ComponentCategory{
			Category:   cc.Name,
			Order:      cc.Order,
			Components: arr,
		})
	}

	// ------------------------------------------------------------------
	// FINAL RESPONSE
	// ------------------------------------------------------------------
	var lastTime *time.Time
	if dbLastMenu.Valid {
		tmp := dbLastMenu.Time
		lastTime = &tmp
	}

	return &models.MenuResponse{
		Status:          "ok",
		LastMenuUpdate:  lastTime,
		ProductsTypes:   productTypes,
		ComponentsTypes: compTypes,
		Delays:          delays,
	}, nil
}
