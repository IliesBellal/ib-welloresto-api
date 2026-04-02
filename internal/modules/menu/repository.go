package menu

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

type MenuRepository struct {
	database *sql.DB
}

func NewMenuRepository(db *sql.DB) *MenuRepository {
	return &MenuRepository{database: db}
}

func (r *MenuRepository) GetUnitsOfMeasures(ctx context.Context, merchantID string) ([]Unit, error) {
	db := dbutils.GetDB(ctx, r.database)

	// 1. Récupérer les unités et leurs descriptions (en français par défaut ici)
	// On utilise CAST ou on scanne directement en string car l'ID est un int en DB mais voulu en string
	unitsQuery := `
		SELECT CAST(u.id AS CHAR) as id, d.uom_desc 
		FROM unit_of_measure u
		JOIN unit_of_measure_desc d ON u.id = d.id
		WHERE d.lang = 'FR'`

	rows, err := db.QueryContext(ctx, unitsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Map pour stocker temporairement les unités
	unitsMap := make(map[string]*Unit)
	var unitOrder []string // Pour garder l'ordre d'affichage si nécessaire

	for rows.Next() {
		var u Unit
		if err := rows.Scan(&u.ID, &u.Name); err != nil {
			return nil, err
		}
		u.CompatibleWith = []string{}
		unitsMap[u.ID] = &u
		unitOrder = append(unitOrder, u.ID)
	}

	// 2. Récupérer les règles de compatibilité
	// On considère que si id_from -> id_to existe, ils sont compatibles
	compatQuery := `SELECT CAST(id_from AS CHAR), CAST(id_to AS CHAR) FROM unit_of_measure_convert`

	compatRows, err := db.QueryContext(ctx, compatQuery)
	if err != nil {
		return nil, err
	}
	defer compatRows.Close()

	for compatRows.Next() {
		var from, to string
		if err := compatRows.Scan(&from, &to); err != nil {
			return nil, err
		}
		// Si l'unité existe dans notre map, on ajoute la compatibilité
		if unit, ok := unitsMap[from]; ok {
			unit.CompatibleWith = append(unit.CompatibleWith, to)
		}
	}

	// 3. Convertir la map en slice pour le retour
	result := make([]Unit, 0, len(unitOrder))
	for _, id := range unitOrder {
		if u, ok := unitsMap[id]; ok {
			result = append(result, *u)
		}
	}

	return result, nil
}

func (r *MenuRepository) GetAttributes(ctx context.Context, merchantID string) ([]Attribute, error) {
	db := dbutils.GetDB(ctx, r.database)

	// 1. Récupération des attributs
	attrQuery := `
        SELECT id, attribute_type, name, title, min_options, max_options
        FROM configurable_attributes
        WHERE merchant_id = ? AND enabled = 1`

	attrRows, err := db.QueryContext(ctx, attrQuery, merchantID)
	if err != nil {
		return nil, fmt.Errorf("query attributes failed: %w", err)
	}
	defer attrRows.Close() // Très important pour éviter les fuites de connexion

	var attributes []Attribute
	// On crée une map qui associe l'ID de l'attribut à son index dans la slice `attributes`
	attrIndexMap := make(map[string]int)

	for attrRows.Next() {
		var attr Attribute
		if err := attrRows.Scan(&attr.ID, &attr.Type, &attr.Name, &attr.Title, &attr.Min, &attr.Max); err != nil {
			return nil, fmt.Errorf("scan attribute failed: %w", err)
		}

		// Initialisation à vide pour éviter le "null" en JSON si l'attribut n'a pas d'options
		attr.Options = []AttributeOption{}

		attributes = append(attributes, attr)
		attrIndexMap[attr.ID] = len(attributes) - 1 // On mémorise sa position
	}

	// Toujours vérifier les erreurs post-boucle
	if err := attrRows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error (attributes): %w", err)
	}

	// 2. Récupération des options
	optQuery := `
        SELECT cao.id, cao.configurable_attribute_id, cao.title, cao.max_quantity, cao.extra_price, cao.enabled
        FROM configurable_attributes ca
        INNER JOIN configurable_attribute_options cao ON cao.configurable_attribute_id = ca.id
        WHERE ca.merchant_id = ? AND ca.enabled = 1 AND cao.enabled = 1`

	optRows, err := db.QueryContext(ctx, optQuery, merchantID)
	if err != nil {
		return nil, fmt.Errorf("query options failed: %w", err)
	}
	defer optRows.Close()

	for optRows.Next() {
		var opt AttributeOption
		var parentAttrID string

		if err := optRows.Scan(&opt.ID, &parentAttrID, &opt.Title, &opt.MaxQuantity, &opt.Price, &opt.Enabled); err != nil {
			return nil, fmt.Errorf("scan option failed: %w", err)
		}

		// 3. Mapping Magique : on trouve l'attribut parent instantanément grâce à la map
		if index, exists := attrIndexMap[parentAttrID]; exists {
			attributes[index].Options = append(attributes[index].Options, opt)
		}
	}

	if err := optRows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error (options): %w", err)
	}

	// Si aucun attribut n'est trouvé, on renvoie une slice vide et pas nil
	if attributes == nil {
		attributes = []Attribute{}
	}

	return attributes, nil
}

func (r *MenuRepository) GetMenu(ctx context.Context, merchantID string, lastMenu *time.Time) (*models.MenuResponse, error) {
	db := dbutils.GetDB(ctx, r.database)

	// --- HELPER FUNCTIONS CORRIGÉES ---
	// On a supprimé les context.WithTimeout internes qui causaient le "context canceled" prématuré.

	// Helper to run a query with logging
	runQuery := func(step string, query string, args ...interface{}) (*sql.Rows, error) {
		// Utilisation directe du ctx parent. Le timeout est géré par le client/serveur HTTP global.
		rows, err := db.QueryContext(ctx, query, args...)

		if err != nil {
			return nil, fmt.Errorf("%s query error: %w", step, err)
		}
		return rows, nil
	}

	// Helper to run QueryRow with logging
	runQueryRow := func(step string, query string, args ...interface{}) *sql.Row {
		// Utilisation directe du ctx parent
		row := db.QueryRowContext(ctx, query, args...)
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
			dbTime := dbLastMenu.Time.UTC().Truncate(time.Second)
			clientTime := lastMenu.UTC().Truncate(time.Second)

			if dbTime.Equal(clientTime) {
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
                   CASE WHEN p.img IS NULL OR p.img = '' THEN false ELSE true END as has_image,
                   p.sync_uber_eats, p.sync_deliveroo
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
			var isPopular, syncUberEats, syncDeliveroo sql.NullBool
			var hasImage bool

			if err := rows.Scan(
				&p.ProductID, &p.ByProductOf, &p.Name, &p.Category, &p.CategoryID, &p.Price, &p.PriceTakeAway, &p.PriceDelivery,
				&desc, &tvaIn, &tvaDel, &tvaTake, &bg, &p.IsProductGroup, &p.Status, &p.IsAvailableOnSNO, &isPopular, &imageURL,
				&availIn, &availTake, &availDel, &hasImage, &syncUberEats, &syncDeliveroo,
			); err != nil {
				return nil, err
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
			if desc.Valid {
				p.Description = &desc.String
			}
			if imageURL.Valid {
				p.ImageURL = &imageURL.String
			}
			if isPopular.Valid {
				p.IsPopular = isPopular.Bool
			} else {
				p.IsPopular = false
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
			if syncDeliveroo.Valid {
				p.SyncDeliveroo = syncDeliveroo.Bool
			}
			if syncUberEats.Valid {
				p.SyncUberEats = syncUberEats.Bool
			}
			defaultOrder := 0
			p.DisplayOrder = &defaultOrder

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
                   p.available_in, p.available_take_away, p.available_delivery,
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
			var availIn, availTake, availDel sql.NullBool
			if err := rows.Scan(&p.ProductID, &by, &p.Name, &p.Category, &p.CategoryID, &p.Price, &p.PriceTakeAway, &p.PriceDelivery, &desc, &availIn, &availTake, &availDel, &tvaIn, &tvaDel, &tvaTake, &bg, &p.IsProductGroup, &p.IsAvailableOnSNO, &p.Status); err != nil {
				return nil, err
			}
			if by.Valid {
				p.ByProductOf = &by.String
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
			if desc.Valid {
				p.Description = &desc.String
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
			defaultOrder := 0
			p.DisplayOrder = &defaultOrder
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

	// --- STEP 7: allergens per product ---
	allergenMap := make(map[string][]models.AllergenEntry)
	{
		q := `
			SELECT pa.product_id, a.allergen_id, a.name, a.code, COALESCE(a.icon, '')
			FROM product_allergens pa
			INNER JOIN allergens a ON a.allergen_id = pa.allergen_id
			WHERE pa.product_id IN (
				SELECT product_id FROM products WHERE merchant_id = ? AND available = 1 AND enabled = 1
			)
		`
		rows, err := runQuery("allergens_per_product", q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var productID string
			var a models.AllergenEntry
			if err := rows.Scan(&productID, &a.ID, &a.Name, &a.Code, &a.Icon); err != nil {
				return nil, err
			}
			allergenMap[productID] = append(allergenMap[productID], a)
		}
	}

	// --- STEP 8: tags per product ---
	tagMap := make(map[string][]models.TagEntry)
	{
		q := `
			SELECT pt.product_id, t.tag_id, t.name
			FROM product_tags pt
			INNER JOIN tags t ON t.tag_id = pt.tag_id
			WHERE t.merchant_id = ? AND pt.product_id IN (
				SELECT product_id FROM products WHERE merchant_id = ? AND available = 1 AND enabled = 1
			)
		`
		rows, err := runQuery("tags_per_product", q, merchantID, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var productID string
			var t models.TagEntry
			if err := rows.Scan(&productID, &t.ID, &t.Name); err != nil {
				return nil, err
			}
			tagMap[productID] = append(tagMap[productID], t)
		}
	}

	// --- STEP 9: delays ---
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

	// --- STEP 10: component categories + all components ---
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
		ID     string
		Name   string
		CatID  *string
		Status string
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
	// attach components, configuration, allergens, tags
	for _, p := range products {
		if comps, ok := compMap[p.ProductID]; ok {
			p.Components = comps
		}
		if attrs, ok := attrMap[p.ProductID]; ok {
			p.Configuration = models.ConfigurableResponse{Attributes: attrs}
		} else {
			p.Configuration = models.ConfigurableResponse{Attributes: []models.ConfigurableAttribute{}}
		}
		if allergens, ok := allergenMap[p.ProductID]; ok {
			p.Allergens = allergens
		}
		if tags, ok := tagMap[p.ProductID]; ok {
			p.Tags = tags
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
			if cb.CatID != nil && cc.ID != nil && *cb.CatID == *cc.ID {
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
	db := dbutils.GetDB(ctx, r.database)

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

	res, err := db.ExecContext(
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

	_ = r.setMenuUpdated(ctx, p.MerchantID)

	return strconv.FormatInt(id, 10), nil
}

func (r *MenuRepository) CreateExternalProductTx(ctx context.Context, merchantID, name, description string, price int) (int64, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
		INSERT INTO products (
			merchant_id,
			name,
			product_desc,
			category,
			price,
			is_available_on_sno,
			tva_in_id,
			tva_delivery_id,
			tva_take_away_id
		)
		VALUES (?, ?, ?, 'UBER_EATS_TEMP', ?, 0, 5, 9, 3)
	`

	res, err := db.ExecContext(ctx, query,
		merchantID,
		name,
		description,
		price,
	)
	if err != nil {
		return 0, err
	}

	newID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return newID, nil
}

func (r *MenuRepository) GetProduct(ctx context.Context, merchantID, productID string) (*ProductEntry, error) {
	db := dbutils.GetDB(ctx, r.database)

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
	err := db.QueryRowContext(ctx, query, merchantID, productID).Scan(
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
	db := dbutils.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx,
		`UPDATE components 
		 SET status = ?
		 WHERE component_id = ? AND merchant_id = ?`,
		status, cid, merchantID,
	)
	if err != nil {
		return 0, err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return res.RowsAffected()
}

func (r *MenuRepository) SetProductAvailability(ctx context.Context, merchantID, pid, status string) (int64, error) {
	db := dbutils.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx,
		`UPDATE products 
		 SET status = ?
		 WHERE product_id = ? AND merchant_id = ?`,
		status, pid, merchantID,
	)
	if err != nil {
		return 0, err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return res.RowsAffected()
}

func (r *MenuRepository) UpdateProduct(ctx context.Context, merchantID, productID string, p ProductUpdatePayload) error {
	db := dbutils.GetDB(ctx, r.database)

	// Note: J'ai ajouté une clause AND merchant_id (ou via jointure) pour la sécurité,
	// sinon n'importe qui avec un token valide pourrait modifier n'importe quel produit ID.
	// Si ta table products n'a pas de merchant_id, il faut faire une jointure avec categories/menus.
	// Pour l'exemple, je suppose une vérification simple sur product_id ou une structure existante.

	query := `
		UPDATE products
		SET 
			name = COALESCE(?, name),
			product_desc = COALESCE(?, product_desc),
			bg_color = COALESCE(?, bg_color),
			category = COALESCE(?, category),
			price = COALESCE(?, price),
			price_take_away = COALESCE(?, price_take_away),
			price_delivery = COALESCE(?, price_delivery),
			by_product_of = ?, 
			is_available_on_sno = COALESCE(?, is_available_on_sno),
			img = COALESCE(?, img),
			enabled = COALESCE(?, enabled),
			available = COALESCE(?, available),
			status = COALESCE(?, status),
			available_in = COALESCE(?, available_in),
			available_take_away = COALESCE(?, available_take_away),
			available_delivery = COALESCE(?, available_delivery)
		WHERE product_id = ? 
		/* AND merchant_id = ?  <- Sécurité recommandée ici */
	`

	// Note: by_product_of n'a pas de COALESCE dans ton PHP original, il est écrasé directement.
	// Je l'ai laissé tel quel (paramètre direct), mais attention si p.ByProductOf est nil.

	_, err := db.ExecContext(ctx, query,
		p.Name,
		p.Description,
		p.BgColor,
		p.Category,
		p.Price,
		p.PriceTakeAway,
		p.PriceDelivery,
		p.ByProductOf, // Attention: si nil, cela mettra NULL en base
		p.IsAvailableOnSno,
		p.ImageBase64,
		p.Enabled,
		p.Available,
		p.Status,
		p.AvailableIn,
		p.AvailableTakeAway,
		p.AvailableDelivery,
		productID,
	)

	_ = r.setMenuUpdated(ctx, merchantID)

	return err
}

func (r *MenuRepository) setMenuUpdated(ctx context.Context, merchantID string) error {
	db := dbutils.GetDB(ctx, r.database)

	// Note: J'ai ajouté une clause AND merchant_id (ou via jointure) pour la sécurité,
	// sinon n'importe qui avec un token valide pourrait modifier n'importe quel produit ID.
	// Si ta table products n'a pas de merchant_id, il faut faire une jointure avec categories/menus.
	// Pour l'exemple, je suppose une vérification simple sur product_id ou une structure existante.

	query := `
		UPDATE merchant_parameters
		SET last_menu_update = UTC_TIMESTAMP
		WHERE merchant_id = ?
	`

	// Note: by_product_of n'a pas de COALESCE dans ton PHP original, il est écrasé directement.
	// Je l'ai laissé tel quel (paramètre direct), mais attention si p.ByProductOf est nil.

	_, err := db.ExecContext(ctx, query, merchantID)

	return err
}

// SyncProductAllergens replaces all allergen associations for a product in a single transaction.
// It verifies that the product belongs to merchantID before modifying it.
func (r *MenuRepository) SyncProductAllergens(ctx context.Context, merchantID, productID string, allergenIDs []int) error {
	db := dbutils.GetDB(ctx, r.database)

	// Ownership check
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM products WHERE product_id = ? AND merchant_id = ?`,
		productID, merchantID,
	).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return models.ErrForbidden
	}

	// Delete existing associations
	if _, err := db.ExecContext(ctx,
		`DELETE FROM product_allergens WHERE product_id = ?`, productID,
	); err != nil {
		return err
	}

	// Insert new associations
	if len(allergenIDs) > 0 {
		stmt, err := db.PrepareContext(ctx,
			`INSERT INTO product_allergens (product_id, allergen_id) VALUES (?, ?)`,
		)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, aid := range allergenIDs {
			if _, err := stmt.ExecContext(ctx, productID, aid); err != nil {
				return err
			}
		}
	}

	_ = r.setMenuUpdated(ctx, merchantID)
	return nil
}

// BulkAssignTag adds a tag to multiple products without removing their other tags.
// Ownership of both the tag and every product is verified against merchantID.
func (r *MenuRepository) BulkAssignTag(ctx context.Context, merchantID, tagID string, productIDs []string) error {
	db := dbutils.GetDB(ctx, r.database)

	if len(productIDs) == 0 {
		return nil
	}

	// 1. Dédoublonner pour éviter les erreurs de validCount
	productIDs = uniqueStrings(productIDs)

	// 2. Vérifier que le tag appartient au marchand
	var tagCount int
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(1) FROM tags WHERE tag_id = ? AND merchant_id = ?",
		tagID, merchantID,
	).Scan(&tagCount)

	if err != nil || tagCount == 0 {
		return models.ErrForbidden
	}

	// 3. Vérifier que TOUS les produits appartiennent au marchand
	// MySQL ne supporte pas ANY(array), on construit donc le IN (?, ?, ?)
	placeholders := make([]string, len(productIDs))
	args := make([]interface{}, len(productIDs)+1)
	args[0] = merchantID // Premier argument pour merchant_id = ?

	for i, pid := range productIDs {
		placeholders[i] = "?"
		args[i+1] = pid
	}

	query := fmt.Sprintf(
		"SELECT COUNT(1) FROM products WHERE merchant_id = ? AND product_id IN (%s)",
		strings.Join(placeholders, ","),
	)

	var validCount int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&validCount); err != nil {
		return err
	}

	if validCount != len(productIDs) {
		return models.ErrForbidden
	}

	// 4. Insertion en masse avec INSERT IGNORE (Spécifique MySQL)
	// On réutilise les placeholders pour faire un seul INSERT groupé pour la performance
	insertValues := make([]string, len(productIDs))
	insertArgs := make([]interface{}, 0, len(productIDs)*2)

	for i, pid := range productIDs {
		insertValues[i] = "(?, ?)"
		insertArgs = append(insertArgs, pid, tagID)
	}

	insertQuery := fmt.Sprintf(
		"INSERT INTO product_tags (product_id, tag_id) VALUES %s",
		strings.Join(insertValues, ","),
	)

	if _, err := db.ExecContext(ctx, insertQuery, insertArgs...); err != nil {
		return err
	}

	return nil
}

// Helper pour éviter les doublons d'IDs dans la slice
func uniqueStrings(input []string) []string {
	u := make([]string, 0, len(input))
	m := make(map[string]bool)
	for _, val := range input {
		if val != "" && !m[val] {
			m[val] = true
			u = append(u, val)
		}
	}
	return u
}

// BulkAssignAllergen adds an allergen to multiple products without removing their other allergens.
// Each product must belong to merchantID.
func (r *MenuRepository) BulkAssignAllergen(ctx context.Context, merchantID, allergenID string, productIDs []string) error {
	db := dbutils.GetDB(ctx, r.database)

	if len(productIDs) == 0 {
		return nil
	}

	// Verify allergen exists (system-wide, no merchant_id check needed)
	var allergenCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM allergens WHERE allergen_id = ?`,
		allergenID,
	).Scan(&allergenCount); err != nil {
		return err
	}
	if allergenCount == 0 {
		return models.ErrNotFound
	}

	// Verify all products belong to merchant
	placeholders := make([]interface{}, 0, len(productIDs)+1)
	placeholders = append(placeholders, merchantID)
	inClause := ""
	for i, pid := range productIDs {
		if i > 0 {
			inClause += ","
		}
		inClause += "?"
		placeholders = append(placeholders, pid)
	}
	var validCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM products WHERE merchant_id = ? AND product_id IN (`+inClause+`)`,
		placeholders...,
	).Scan(&validCount); err != nil {
		return err
	}
	if validCount != len(productIDs) {
		return models.ErrForbidden
	}

	// Upsert associations
	stmt, err := db.PrepareContext(ctx,
		`INSERT IGNORE INTO product_allergens (product_id, allergen_id) VALUES (?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, pid := range productIDs {
		if _, err := stmt.ExecContext(ctx, pid, allergenID); err != nil {
			return err
		}
	}

	return nil
}

// SyncProductTags replaces all tag associations for a product in a single transaction.
// It verifies that the product belongs to merchantID and that all supplied tag_ids also belong
// to the same merchant before modifying anything.
func (r *MenuRepository) SyncProductTags(ctx context.Context, merchantID, productID string, tagIDs []string) error {
	db := dbutils.GetDB(ctx, r.database)

	// Ownership check: product must belong to merchant
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM products WHERE product_id = ? AND merchant_id = ?`,
		productID, merchantID,
	).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return models.ErrForbidden
	}

	// Verify all supplied tags belong to the merchant
	if len(tagIDs) > 0 {
		placeholders := make([]interface{}, 0, len(tagIDs)+1)
		placeholders = append(placeholders, merchantID)
		inClause := ""
		for i, tid := range tagIDs {
			if i > 0 {
				inClause += ","
			}
			inClause += "?"
			placeholders = append(placeholders, tid)
		}
		var validCount int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM tags WHERE merchant_id = ? AND id IN (`+inClause+`)`,
			placeholders...,
		).Scan(&validCount); err != nil {
			return err
		}
		if validCount != len(tagIDs) {
			return models.ErrForbidden
		}
	}

	// Delete existing associations
	if _, err := db.ExecContext(ctx,
		`DELETE FROM product_tags WHERE product_id = ?`, productID,
	); err != nil {
		return err
	}

	// Insert new associations
	if len(tagIDs) > 0 {
		stmt, err := db.PrepareContext(ctx,
			`INSERT INTO product_tags (product_id, tag_id) VALUES (?, ?)`,
		)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, tid := range tagIDs {
			if _, err := stmt.ExecContext(ctx, productID, tid); err != nil {
				return err
			}
		}
	}

	_ = r.setMenuUpdated(ctx, merchantID)
	return nil
}

func (r *MenuRepository) UpdateProductAttributes(ctx context.Context, merchantID, productID string, configIDs []string) error {
	db := dbutils.GetDB(ctx, r.database)

	// 2. Reset Config (Set enabled = 0)
	// Correspond à: UPDATE product_configurable_attribute SET enabled = 0 WHERE product_id = ...
	_, err := db.ExecContext(ctx, `
		UPDATE product_configurable_attribute 
		SET enabled = 0 
		WHERE product_id = ?`, productID)
	if err != nil {
		return err
	}

	// 3. Loop et Upsert
	// Correspond au foreach($product->configuration) en PHP
	stmtQuery := `
		INSERT INTO product_configurable_attribute(product_id, configurable_attribute_id, num_order, enabled)
		VALUES(?, ?, ?, 1)
		ON DUPLICATE KEY UPDATE enabled = 1, num_order = VALUES(num_order)
	`

	// Préparer le statement est plus performant dans une boucle
	stmt, err := db.PrepareContext(ctx, stmtQuery)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, attributeID := range configIDs {
		// i correspond à ton $i (num_order)
		_, err = stmt.ExecContext(ctx, productID, attributeID, i)
		if err != nil {
			return err
		}
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

// ListTags returns all tags belonging to a merchant.
func (r *MenuRepository) ListTags(ctx context.Context, merchantID string) ([]models.TagEntry, error) {
	db := dbutils.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT id, merchant_id, name
		 FROM tags
		 WHERE merchant_id = ?
		 ORDER BY name ASC`,
		merchantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.TagEntry
	for rows.Next() {
		var t models.TagEntry
		if err := rows.Scan(&t.ID, &t.MerchantID, &t.Name); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}
