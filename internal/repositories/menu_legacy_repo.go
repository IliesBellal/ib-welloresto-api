package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type LegacyMenuRepository struct {
	db  *sql.DB
	log *zap.Logger
}

func NewLegacyMenuRepository(db *sql.DB, log *zap.Logger) *LegacyMenuRepository {
	return &LegacyMenuRepository{db: db, log: log}
}

func (r *LegacyMenuRepository) GetMenu(ctx context.Context, merchantID string, lastMenu *time.Time) (*models.MenuResponse, error) {
	startTotal := time.Now()
	r.log.Info("GetMenu START", zap.String("merchant_id", merchantID), zap.Time("start_at", startTotal))

	// begin transaction (read-only)
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		r.log.Error("BeginTx failed", zap.Error(err))
		return nil, fmt.Errorf("BeginTx failed: %w", err)
	}
	// ensure rollback if anything goes wrong
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// helper to run a query with per-query timeout and logging
	runQuery := func(step string, query string, args ...interface{}) (*sql.Rows, error) {
		// per-query timeout short (diagnostic). Adjust if necessary.
		qctx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		r.log.Info("Query START", zap.String("step", step))
		t0 := time.Now()
		rows, err := tx.QueryContext(qctx, query, args...)
		elapsed := time.Since(t0)
		if err != nil {
			r.log.Error("Query ERROR", zap.String("step", step), zap.Duration("elapsed", elapsed), zap.Error(err))
			return nil, fmt.Errorf("%s query error: %w", step, err)
		}
		r.log.Info("Query DONE", zap.String("step", step), zap.Duration("elapsed", elapsed))
		return rows, nil
	}

	// helper to run QueryRow with timeout
	runQueryRow := func(step string, query string, args ...interface{}) *sql.Row {
		qctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		// caller must not call cancel; we cancel after returning (use closure)
		// but since sql.Row doesn't accept ctx cancellation directly, we use tx.QueryRowContext
		defer cancel()
		r.log.Info("QueryRow START", zap.String("step", step))
		t0 := time.Now()
		row := tx.QueryRowContext(qctx, query, args...)
		r.log.Info("QueryRow queued", zap.String("step", step), zap.Duration("elapsed_since_start", time.Since(t0)))
		return row
	}

	// --- STEP 1: last_menu_update ---
	var dbLastMenu sql.NullTime
	{
		step := "last_menu_update"
		q := "SELECT last_menu_update FROM merchant_parameters WHERE merchant_id = ? LIMIT 1"
		row := runQueryRow(step, q, merchantID)
		if err := row.Scan(&dbLastMenu); err != nil && err != sql.ErrNoRows {
			r.log.Error("Scan last_menu_update failed", zap.Error(err))
			return nil, fmt.Errorf("scan last_menu_update failed: %w", err)
		}
		r.log.Info("last_menu_update fetched", zap.String("merchant_id", merchantID), zap.Bool("valid", dbLastMenu.Valid))
		// quick equality check
		if lastMenu != nil && dbLastMenu.Valid {
			if dbLastMenu.Time.Format("2006-01-02 15:04:05") == lastMenu.Format("2006-01-02 15:04:05") {
				if err := tx.Commit(); err != nil {
					r.log.Error("tx.Commit error (no_update_required)", zap.Error(err))
					return nil, err
				}
				committed = true
				r.log.Info("GetMenu END - no_update_required", zap.Duration("total_elapsed", time.Since(startTotal)))
				return &models.MenuResponse{Status: "no_update_required"}, nil
			}
		}
	}

	// --- STEP 2: categories ---
	var cats []struct {
		ID    *string
		Name  string
		Order int
		Bg    sql.NullString
	}
	{
		step := "categories"
		q := `
            SELECT pc.merchant_categ_id, pc.categ_name, pc.categ_order, pc.bg_color
            FROM productcateg pc
            WHERE pc.available = 1 AND pc.enabled = 1 AND pc.merchant_id = ?
            ORDER BY pc.categ_order ASC
        `
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var c struct {
				ID    *string
				Name  string
				Order int
				Bg    sql.NullString
			}
			if err := rows.Scan(&c.ID, &c.Name, &c.Order, &c.Bg); err != nil {
				r.log.Error("categories scan failed", zap.Error(err))
				return nil, err
			}
			cats = append(cats, c)
			count++
		}
		r.log.Info("categories loaded", zap.Int("rows", count))
	}

	// --- STEP 3: products (roots) ---
	type prodTmp struct {
		models.ProductEntry
	}
	products := make(map[string]*models.ProductEntry)
	var productOrder []string
	{
		step := "products_roots"
		q := `
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
        `
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var p models.ProductEntry
			var tvaIn, tvaDel, tvaTake sql.NullFloat64
			var bg sql.NullString
			var desc sql.NullString
			var imageURL sql.NullString
			var availIn, availTake, availDel sql.NullBool
			var isPopular sql.NullBool
			var hasImage bool

			if err := rows.Scan(
				&p.ProductID, &p.ByProductOf, &p.Name, &p.Category, &p.Price, &p.PriceTakeAway, &p.PriceDelivery,
				&desc, &tvaIn, &tvaDel, &tvaTake, &bg, &p.IsProductGroup, &p.Status, &p.IsAvailableOnSNO, &isPopular, &imageURL,
				&availIn, &availTake, &availDel, &hasImage,
			); err != nil {
				r.log.Error("products_roots scan failed", zap.Error(err))
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
			count++
		}
		r.log.Info("products_roots loaded", zap.Int("rows", count))
	}

	// --- STEP 4: sub-products ---
	subProducts := make(map[string]*models.ProductEntry)
	{
		step := "sub_products"
		q := `
            SELECT p.product_id, p.by_product_of, p.name, p.category, p.price, p.price_take_away, p.price_delivery, p.product_desc,
                   tva_in.tva_rate as tva_rate_in, tva_delivery.tva_rate as tva_rate_delivery, tva_take_away.tva_rate as tva_rate_take_away, p.bg_color, p.is_product_group, p.is_available_on_sno, p.status
            FROM products p
            INNER JOIN tva_categories tva_in on tva_in.tva_id = p.tva_in_id
            INNER JOIN tva_categories tva_delivery on tva_delivery.tva_id = p.tva_delivery_id
            INNER JOIN tva_categories tva_take_away on tva_take_away.tva_id = p.tva_take_away_id
            WHERE p.merchant_id = ? AND p.by_product_of IS NOT NULL AND p.available = 1
        `
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var p models.ProductEntry
			var by sql.NullString
			var tvaIn, tvaDel, tvaTake sql.NullFloat64
			var bg sql.NullString
			var desc sql.NullString
			if err := rows.Scan(&p.ProductID, &by, &p.Name, &p.Category, &p.Price, &p.PriceTakeAway, &p.PriceDelivery, &desc, &tvaIn, &tvaDel, &tvaTake, &bg, &p.IsProductGroup, &p.IsAvailableOnSNO, &p.Status); err != nil {
				r.log.Error("sub_products scan failed", zap.Error(err))
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
			count++
		}
		r.log.Info("sub_products loaded", zap.Int("rows", count))
	}

	// --- STEP 5: components (requires) ---
	compMap := make(map[string][]models.ComponentUsage)
	{
		step := "components_requires"
		q := `
            SELECT r.product_id, c.component_id, c.name, c.component_price, c.status, rq.quantity, uomd.uom_desc
            FROM components c
            INNER JOIN requires rq on c.component_id = rq.component_id and rq.enabled = true
            INNER JOIN recipes r on r.recipe_id = rq.recipe_id
            INNER JOIN unit_of_measure_desc uomd on uomd.lang = 'FR' and uomd.id = rq.unit_of_measure
            WHERE c.merchant_id = ? AND c.available = 1 AND rq.enabled = true
        `
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var productID string
			var c models.ComponentUsage
			var uom sql.NullString
			if err := rows.Scan(&productID, &c.ComponentID, &c.Name, &c.Price, &c.Status, &c.Quantity, &uom); err != nil {
				r.log.Error("components_requires scan failed", zap.Error(err))
				return nil, err
			}
			if uom.Valid {
				c.UnitOfMeasure = uom.String
			}
			compMap[productID] = append(compMap[productID], c)
			count++
		}
		r.log.Info("components_requires loaded", zap.Int("rows", count))
	}

	// --- STEP 6: configurable attributes + options (we load options then attrs like PHP) ---
	optMap := make(map[string][]models.ConfigurableOption)
	{
		step := "configurable_options"
		q := `
            SELECT DISTINCT ca.id as configurable_attribute_id, cao.id, cao.title, cao.extra_price, cao.max_quantity
            FROM products p
            INNER JOIN product_configurable_attribute pca on pca.product_id = p.product_id
            INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
            INNER JOIN configurable_attribute_options cao on cao.configurable_attribute_id = ca.id
            WHERE p.merchant_id = ? AND ca.enabled = 1 AND cao.enabled = 1 AND pca.enabled = 1
        `
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var cfgID string
			var o models.ConfigurableOption
			if err := rows.Scan(&cfgID, &o.ID, &o.Title, &o.ExtraPrice, &o.MaxQuantity); err != nil {
				r.log.Error("configurable_options scan failed", zap.Error(err))
				return nil, err
			}
			optMap[cfgID] = append(optMap[cfgID], o)
			count++
		}
		r.log.Info("configurable_options loaded", zap.Int("rows", count))
	}

	attrMap := make(map[string][]models.ConfigurableAttribute)
	{
		step := "configurable_attributes"
		q := `
            SELECT ca.id, pca.product_id, ca.title, ca.max_options, ca.attribute_type, ca.min_options
            FROM products p
            INNER JOIN product_configurable_attribute pca on pca.product_id = p.product_id
            INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
            WHERE p.merchant_id = ? AND ca.enabled = 1 AND pca.enabled = 1
            ORDER BY pca.num_order ASC
        `
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var a models.ConfigurableAttribute
			if err := rows.Scan(&a.ID, &a.ProductID, &a.Title, &a.MaxOptions, &a.AttributeType, &a.MinOptions); err != nil {
				r.log.Error("configurable_attributes scan failed", zap.Error(err))
				return nil, err
			}
			a.Options = optMap[a.ID]
			attrMap[a.ProductID] = append(attrMap[a.ProductID], a)
			count++
		}
		r.log.Info("configurable_attributes loaded", zap.Int("rows", count))
	}

	// --- STEP 7: delays ---
	var delays []models.DelayEntry
	{
		step := "delays"
		q := `SELECT id, short_description, duration FROM delays WHERE enabled = true ORDER BY duration ASC`
		rows, err := runQuery(step, q)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var d models.DelayEntry
			if err := rows.Scan(&d.DelayID, &d.ShortDescription, &d.Duration); err != nil {
				r.log.Error("delays scan failed", zap.Error(err))
				return nil, err
			}
			delays = append(delays, d)
			count++
		}
		r.log.Info("delays loaded", zap.Int("rows", count))
	}

	// --- STEP 8: component categories + all components ---
	type compCatTmp struct {
		ID    int64
		Name  string
		Order int
	}
	var compCats []compCatTmp
	{
		step := "component_categories"
		q := `SELECT merchant_categ_id, name, categ_order FROM component_category WHERE merchant_id = ? AND available = 1 ORDER BY categ_order ASC`
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var c compCatTmp
			if err := rows.Scan(&c.ID, &c.Name, &c.Order); err != nil {
				r.log.Error("component_categories scan failed", zap.Error(err))
				return nil, err
			}
			compCats = append(compCats, c)
			count++
		}
		r.log.Info("component_categories loaded", zap.Int("rows", count))
	}

	type compBasicTmp struct {
		ID     int64
		Name   string
		CatID  int64
		Status int
		Price  int
	}
	var allComponents []compBasicTmp
	{
		step := "all_components"
		q := `SELECT component_id, name, category_id, status, component_price FROM components WHERE merchant_id = ?`
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var cb compBasicTmp
			if err := rows.Scan(&cb.ID, &cb.Name, &cb.CatID, &cb.Status, &cb.Price); err != nil {
				r.log.Error("all_components scan failed", zap.Error(err))
				return nil, err
			}
			allComponents = append(allComponents, cb)
			count++
		}
		r.log.Info("all_components loaded", zap.Int("rows", count))
	}

	// --- BUILD: attach sub-products to parents & attach components & configuration like PHP ---
	buildStart := time.Now()
	// attach subproducts
	for _, sp := range subProducts {
		if sp.ByProductOf != nil {
			if parent, ok := products[*sp.ByProductOf]; ok && parent != nil {
				parent.SubProducts = append(parent.SubProducts, *sp)
			}
		}
	}
	// attach components & configuration
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
	r.log.Info("build phase done", zap.Duration("elapsed", time.Since(buildStart)))

	// --- build categories -> products (respect categ_order + productOrder) ---
	productTypes := []models.ProductCategory{}
	for _, c := range cats {
		actual := []models.ProductEntry{}
		for _, pid := range productOrder {
			if p, ok := products[pid]; ok && p != nil && p.Category == c.ID {
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

	// --- build component types ---
	compTypes := []models.ComponentCategory{}
	for _, cc := range compCats {
		actual := []models.ComponentBasic{}
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
			Category:   cc.Name,
			Order:      cc.Order,
			Components: actual,
		})
	}

	// commit transaction
	if err := tx.Commit(); err != nil {
		r.log.Error("tx.Commit failed", zap.Error(err))
		return nil, err
	}
	committed = true

	// prepare response
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

	r.log.Info("GetMenu END", zap.Duration("total_elapsed", time.Since(startTotal)), zap.Int("categories", len(cats)), zap.Int("products", len(productOrder)))
	return resp, nil
}
