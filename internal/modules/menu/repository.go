package menu

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

type MenuRepository struct {
	db *sql.DB
}

func NewMenuRepository(db *sql.DB) *MenuRepository {
	return &MenuRepository{db: db}
}

func (r *MenuRepository) GetUnitsOfMeasures(ctx context.Context, merchantID string) ([]Unit, error) {
	return []Unit{
		{ID: 10, Name: "Grammes (g)", CompatibleWith: []string{"10", "11", "12"}},
		{ID: 11, Name: "Kilogrammes (kg)", CompatibleWith: []string{"10", "11", "12"}},
		{ID: 12, Name: "Milligrammes (mg)", CompatibleWith: []string{"10", "11", "12"}},
		{ID: 20, Name: "Litres (L)", CompatibleWith: []string{"20", "21"}},
		{ID: 21, Name: "Centilitres (cL)", CompatibleWith: []string{"20", "21"}},
	}, nil
}

func (r *MenuRepository) GetAttributes(ctx context.Context, merchantID string) ([]Attribute, error) {
	return []Attribute{
		{
			ID:    "attr_1",
			Title: "Taille Pizza",
			Type:  "CHECK",
			Min:   1,
			Max:   1,
			Options: []AttributeOption{
				{ID: "opt_1", Title: "Junior", Price: 0},
				{ID: "opt_2", Title: "Senior", Price: 200},
				{ID: "opt_3", Title: "Mega", Price: 500},
			},
		},
		{
			ID:    "attr_2",
			Title: "Suppléments",
			Type:  "CHECK",
			Min:   0,
			Max:   5,
			Options: []AttributeOption{
				{ID: "opt_4", Title: "Olive", Price: 50},
				{ID: "opt_5", Title: "Oeuf", Price: 100},
			},
		},
	}, nil
}

func (r *MenuRepository) GetMenu(ctx context.Context, merchantID string, lastMenu *time.Time) (*models.MenuResponse, error) {

	// Begin transaction (read-only)
	// Note: On utilise le ctx parent. Si la requête HTTP est annulée, la transaction s'arrêtera proprement.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("BeginTx failed: %w", err)
	}

	// Ensure rollback if anything goes wrong
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// --- HELPER FUNCTIONS CORRIGÉES ---
	// On a supprimé les context.WithTimeout internes qui causaient le "context canceled" prématuré.

	// Helper to run a query with logging
	runQuery := func(step string, query string, args ...interface{}) (*sql.Rows, error) {
		// Utilisation directe du ctx parent. Le timeout est géré par le client/serveur HTTP global.
		rows, err := tx.QueryContext(ctx, query, args...)

		if err != nil {
			return nil, fmt.Errorf("%s query error: %w", step, err)
		}
		return rows, nil
	}

	// Helper to run QueryRow with logging
	runQueryRow := func(step string, query string, args ...interface{}) *sql.Row {
		// Utilisation directe du ctx parent
		row := tx.QueryRowContext(ctx, query, args...)
		return row
	}

	// --- STEP 1: last_menu_update ---
	var dbLastMenu sql.NullTime
	{
		step := "last_menu_update"
		q := "SELECT last_menu_update FROM merchant_parameters WHERE merchant_id = ? LIMIT 1"

		// Ici, le Scan va fonctionner car le contexte n'est plus annulé par le helper
		row := runQueryRow(step, q, merchantID)
		if err := row.Scan(&dbLastMenu); err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("scan last_menu_update failed: %w", err)
		}

		// quick equality check
		if lastMenu != nil && dbLastMenu.Valid {
			if dbLastMenu.Time.Format("2006-01-02 15:04:05") == lastMenu.Format("2006-01-02 15:04:05") {
				if err := tx.Commit(); err != nil {
					return nil, err
				}
				committed = true
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
				return nil, err
			}
			cats = append(cats, c)
			count++
		}
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
            SELECT p.product_id, p.by_product_of, p.name, p.category, p.category, p.price, p.price_take_away, p.price_delivery, p.product_desc,
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
				&p.ProductID, &p.ByProductOf, &p.Name, &p.Category, &p.CategoryID, &p.Price, &p.PriceTakeAway, &p.PriceDelivery,
				&desc, &tvaIn, &tvaDel, &tvaTake, &bg, &p.IsProductGroup, &p.Status, &p.IsAvailableOnSNO, &isPopular, &imageURL,
				&availIn, &availTake, &availDel, &hasImage,
			); err != nil {
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
	}

	// --- STEP 4: sub-products ---
	subProducts := make(map[string]*models.ProductEntry)
	{
		step := "sub_products"
		q := `
            SELECT p.product_id, p.by_product_of, p.name, p.category, p.category, p.price, p.price_take_away, p.price_delivery, p.product_desc,
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
			if err := rows.Scan(&p.ProductID, &by, &p.Name, &p.Category, &p.CategoryID, &p.Price, &p.PriceTakeAway, &p.PriceDelivery, &desc, &tvaIn, &tvaDel, &tvaTake, &bg, &p.IsProductGroup, &p.IsAvailableOnSNO, &p.Status); err != nil {
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
				return nil, err
			}
			if uom.Valid {
				c.UnitOfMeasure = uom.String
			}
			compMap[productID] = append(compMap[productID], c)
			count++
		}
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
				return nil, err
			}
			optMap[cfgID] = append(optMap[cfgID], o)
			count++
		}
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
				return nil, err
			}
			a.Options = optMap[a.ID]
			attrMap[a.ProductID] = append(attrMap[a.ProductID], a)
			count++
		}
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
				return nil, err
			}
			delays = append(delays, d)
			count++
		}
	}

	// --- STEP 8: component categories + all components ---
	type compCatTmp struct {
		ID    *string
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
				return nil, err
			}
			compCats = append(compCats, c)
			count++
		}
	}

	type compBasicTmp struct {
		ID     int64
		Name   string
		CatID  *string
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
				return nil, err
			}
			allComponents = append(allComponents, cb)
			count++
		}
	}

	// --- BUILD: attach sub-products to parents & attach components & configuration like PHP ---
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

	// --- build categories -> products (respect categ_order + productOrder) ---
	productTypes := []models.ProductCategory{}
	for _, c := range cats {
		actual := []models.ProductEntry{}
		for _, pid := range productOrder {
			if p, ok := products[pid]; ok && p != nil && *p.CategoryID == *c.ID {
				actual = append(actual, *p)
			}
		}
		var bg *string
		if c.Bg.Valid {
			bg = &c.Bg.String
		}
		productTypes = append(productTypes, models.ProductCategory{
			Category:     c.Name,
			CategoryName: c.Name,
			CategoryID:   c.ID,
			Order:        c.Order,
			BgColor:      bg,
			Products:     actual,
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
		return nil, err
	}
	committed = true

	// prepare response
	resp := &models.MenuResponse{
		Status:          "ok",
		LastMenuUpdate:  helpers.NullTimeToNullUnixInt(dbLastMenu),
		ProductsTypes:   productTypes,
		ComponentsTypes: compTypes,
		Delays:          delays,
	}

	return resp, nil
}

func (r *MenuRepository) CreateProduct(ctx context.Context, p *CreateProductPayload) (string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "0", err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO products (
			merchant_id,
			name,
			product_desc,
			price,
			price_take_away,
			price_delivery,
			tva_in_id,
			tva_delivery_id,
			tva_take_away_id,
			category,
			is_product_group
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	res, err := tx.ExecContext(
		ctx,
		query,
		p.MerchantID,
		p.Name,
		p.ProductDesc,
		p.Price,
		p.PriceTakeAway,
		p.PriceDelivery,
		p.TvaInID,
		p.TvaDeliveryID,
		p.TvaTakeAwayID,
		p.Category,
		p.IsProductGroup,
	)
	if err != nil {
		return "0", err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return "0", err
	}

	if err := tx.Commit(); err != nil {
		return "0", err
	}

	return strconv.FormatInt(id, 10), nil
}

func (r *MenuRepository) GetProduct(ctx context.Context, merchantID, productID string) (*ProductEntry, error) {
	query := `
		SELECT
			product_id,
			merchant_id,
			name,
			product_desc,
			price,
			price_take_away,
			price_delivery,
			category,
			is_product_group
		FROM products
		WHERE merchant_id = ? AND product_id = ?
		LIMIT 1
	`

	var p ProductEntry
	err := r.db.QueryRowContext(ctx, query, merchantID, productID).Scan(
		&p.ProductID,
		&p.MerchantID,
		&p.Name,
		&p.Description,
		&p.Price,
		&p.PriceTakeAway,
		&p.PriceDelivery,
		&p.Category,
		&p.IsProductGroup,
	)

	if err != nil {
		return nil, err
	}

	return &p, nil
}

func (r *MenuRepository) SetComponentAvailability(ctx context.Context, merchantID, cid, status string) (int64, error) {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE components 
		 SET status = ?
		 WHERE component_id = ? AND merchant_id = ?`,
		status, cid, merchantID,
	)
	if err != nil {
		tx.Rollback()
		return 0, err
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE merchant_parameters 
		 SET last_menu_update = UTC_TIMESTAMP 
		 WHERE merchant_id = ?`,
		merchantID,
	)
	if err != nil {
		tx.Rollback()
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

func (r *MenuRepository) SetProductAvailability(ctx context.Context, merchantID, pid, status string) (int64, error) {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE products 
		 SET status = ?
		 WHERE product_id = ? AND merchant_id = ?`,
		status, pid, merchantID,
	)
	if err != nil {
		tx.Rollback()
		return 0, err
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE merchant_parameters 
		 SET last_menu_update = UTC_TIMESTAMP 
		 WHERE merchant_id = ?`,
		merchantID,
	)
	if err != nil {
		tx.Rollback()
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return res.RowsAffected()
}
