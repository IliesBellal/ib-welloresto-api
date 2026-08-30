package menu

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/modules/menu/importer"
)

// BuildMerchantCanonicalImport lit le catalogue vivant d'un marchand et le
// traduit en modèle canonique — l'équivalent, pour la porte "autre
// établissement", de ce qu'un importer.ImportProvider fait en parsant un
// fichier. Contrairement aux parsers de fichier, cette fonction ACCÈDE à la
// base ; elle reste néanmoins un simple traducteur, sans arbitrage : aucune
// décision (TVA, catégorie, doublon) n'est prise ici, c'est le rôle de
// BuildPreview/BuildCommitPlan en aval, exactement comme pour un fichier.
//
// sourceMerchantID a déjà été vérifié par l'appelant (ImportService.PreviewImportFromMerchant,
// via merchantRightsChecker) : cette fonction lui fait confiance.
//
// Ordre de lecture imposé par les dépendances qu'un ExternalID doit pouvoir
// résoudre chez l'appelant : catégories produit, catégories d'ingrédient et
// ingrédients (une option peut lier un ingrédient), tags, attributs, puis
// produits — seuls à référencer tout le reste.
func (r *MenuRepository) BuildMerchantCanonicalImport(ctx context.Context, sourceMerchantID string) (*importer.IntermediateImport, error) {
	imp := &importer.IntermediateImport{}

	categories, categIDByMerchantCategID, err := r.loadMerchantCanonicalCategories(ctx, sourceMerchantID)
	if err != nil {
		return nil, err
	}
	imp.Categories = categories

	componentCategories, componentCategIDByMerchantCategID, err := r.loadMerchantCanonicalComponentCategories(ctx, sourceMerchantID)
	if err != nil {
		return nil, err
	}
	imp.ComponentCategories = componentCategories

	components, err := r.loadMerchantCanonicalComponents(ctx, sourceMerchantID, componentCategIDByMerchantCategID)
	if err != nil {
		return nil, err
	}
	imp.Components = components

	tags, err := r.loadMerchantCanonicalTags(ctx, sourceMerchantID)
	if err != nil {
		return nil, err
	}
	imp.Tags = tags

	attributes, err := r.loadMerchantCanonicalAttributes(ctx, sourceMerchantID)
	if err != nil {
		return nil, err
	}
	imp.Attributes = attributes

	tvaRateByID, err := r.loadMerchantTvaRatesByID(ctx)
	if err != nil {
		return nil, err
	}

	products, err := r.loadMerchantCanonicalProducts(ctx, sourceMerchantID, categIDByMerchantCategID, tvaRateByID)
	if err != nil {
		return nil, err
	}
	if len(products) == 0 {
		return nil, importer.ErrNoProducts
	}
	imp.Products = products

	return imp, nil
}

// loadMerchantCanonicalCategories lit les catégories caisse actives du
// marchand source. Le second retour permet de résoudre products.category
// (qui porte merchant_categ_id, pas categ_id) vers l'ExternalID de la
// catégorie — categ_id, comme pour les autres tables de mapping de ce paquet.
func (r *MenuRepository) loadMerchantCanonicalCategories(ctx context.Context, merchantID string) ([]importer.CanonicalCategory, map[string]int, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT categ_id, merchant_categ_id, categ_name
		 FROM productcateg
		 WHERE merchant_id = ? AND enabled = TRUE
		 ORDER BY categ_id ASC`,
		merchantID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("import établissement: chargement des catégories: %w", err)
	}
	defer rows.Close()

	var out []importer.CanonicalCategory
	categIDByMerchantCategID := make(map[string]int)
	for rows.Next() {
		var (
			categID         int
			merchantCategID string
			name            string
		)
		if err := rows.Scan(&categID, &merchantCategID, &name); err != nil {
			return nil, nil, fmt.Errorf("import établissement: lecture d'une catégorie: %w", err)
		}
		out = append(out, importer.CanonicalCategory{ExternalID: strconv.Itoa(categID), Name: name})
		categIDByMerchantCategID[merchantCategID] = categID
	}
	return out, categIDByMerchantCategID, rows.Err()
}

// loadMerchantCanonicalComponentCategories est l'équivalent, pour les
// catégories d'ingrédient, de loadMerchantCanonicalCategories.
func (r *MenuRepository) loadMerchantCanonicalComponentCategories(ctx context.Context, merchantID string) ([]importer.CanonicalComponentCategory, map[string]int, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT id, merchant_categ_id, name
		 FROM component_category
		 WHERE merchant_id = ? AND enabled = TRUE
		 ORDER BY id ASC`,
		merchantID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("import établissement: chargement des catégories d'ingrédient: %w", err)
	}
	defer rows.Close()

	var out []importer.CanonicalComponentCategory
	categIDByMerchantCategID := make(map[string]int)
	for rows.Next() {
		var (
			categID         int
			merchantCategID string
			name            string
		)
		if err := rows.Scan(&categID, &merchantCategID, &name); err != nil {
			return nil, nil, fmt.Errorf("import établissement: lecture d'une catégorie d'ingrédient: %w", err)
		}
		out = append(out, importer.CanonicalComponentCategory{ExternalID: strconv.Itoa(categID), Name: name})
		categIDByMerchantCategID[merchantCategID] = categID
	}
	return out, categIDByMerchantCategID, rows.Err()
}

// loadMerchantCanonicalComponents lit les ingrédients actifs du marchand
// source. componentCategIDByMerchantCategID résout components.category_id
// (merchant_categ_id) vers l'ExternalID de sa catégorie ; un composant dont la
// catégorie référencée n'a pas été chargée (désactivée) repart sans
// CategoryExternalID plutôt que de faire échouer tout l'import — le commit
// distant le traitera comme un composant à recatégoriser, exactement comme un
// produit sans catégorie résolue.
func (r *MenuRepository) loadMerchantCanonicalComponents(
	ctx context.Context,
	merchantID string,
	componentCategIDByMerchantCategID map[string]int,
) ([]importer.CanonicalComponent, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT component_id, name, category_id, unit_of_measure, purchase_unit_id,
		        component_price, purchase_price, purchase_price_quantity,
		        conservation_days, conservation_type, storage_temp_min, storage_temp_max
		 FROM components
		 WHERE merchant_id = ? AND enabled = TRUE
		 ORDER BY component_id ASC`,
		merchantID,
	)
	if err != nil {
		return nil, fmt.Errorf("import établissement: chargement des composants: %w", err)
	}
	defer rows.Close()

	var out []importer.CanonicalComponent
	for rows.Next() {
		var (
			componentID      int
			name             string
			categoryID       sql.NullString
			unitOfMeasure    int
			purchaseUnitID   sql.NullString
			price            int
			purchaseCost     int
			purchaseCostQty  float64
			conservationDays sql.NullInt64
			conservationType sql.NullString
			storageTempMin   sql.NullFloat64
			storageTempMax   sql.NullFloat64
		)
		if err := rows.Scan(&componentID, &name, &categoryID, &unitOfMeasure, &purchaseUnitID,
			&price, &purchaseCost, &purchaseCostQty,
			&conservationDays, &conservationType, &storageTempMin, &storageTempMax); err != nil {
			return nil, fmt.Errorf("import établissement: lecture d'un composant: %w", err)
		}

		component := importer.CanonicalComponent{
			ExternalID:      strconv.Itoa(componentID),
			Name:            name,
			UnitOfMeasureID: strconv.Itoa(unitOfMeasure),
			Price:           price,
			PurchaseCost:    purchaseCost,
			PurchaseCostQty: purchaseCostQty,
		}
		if categoryID.Valid {
			if categExternalID, ok := componentCategIDByMerchantCategID[categoryID.String]; ok {
				component.CategoryExternalID = strconv.Itoa(categExternalID)
			}
		}
		if purchaseUnitID.Valid {
			component.PurchaseUnitOfMeasureID = purchaseUnitID.String
		}
		if conservationDays.Valid {
			days := int(conservationDays.Int64)
			component.ConservationDays = &days
		}
		if conservationType.Valid {
			component.ConservationType = conservationType.String
		}
		if storageTempMin.Valid {
			min := storageTempMin.Float64
			component.StorageTempMin = &min
		}
		if storageTempMax.Valid {
			max := storageTempMax.Float64
			component.StorageTempMax = &max
		}

		out = append(out, component)
	}
	return out, rows.Err()
}

// loadMerchantCanonicalTags lit les tags du marchand source. Ils arrivent déjà
// classés "tag" — CanonicalTag n'a pas de statut catégorie potentielle ici,
// contrairement à un libellé de fichier : le marchand source a lui-même déjà
// distingué ses catégories (productcateg) de ses tags (tags), il n'y a rien à
// reclasser.
func (r *MenuRepository) loadMerchantCanonicalTags(ctx context.Context, merchantID string) ([]importer.CanonicalTag, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT tag_id, name FROM tags WHERE merchant_id = ? ORDER BY tag_id ASC`,
		merchantID,
	)
	if err != nil {
		return nil, fmt.Errorf("import établissement: chargement des tags: %w", err)
	}
	defer rows.Close()

	var out []importer.CanonicalTag
	for rows.Next() {
		var tag importer.CanonicalTag
		if err := rows.Scan(&tag.ExternalID, &tag.Name); err != nil {
			return nil, fmt.Errorf("import établissement: lecture d'un tag: %w", err)
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

// loadMerchantCanonicalAttributes lit les groupes d'options actifs du
// marchand source, options comprises — à la différence des exports fichier,
// la source connaît déjà min/max/type, il n'y a pas d'applyDefaults à
// appliquer en aval.
func (r *MenuRepository) loadMerchantCanonicalAttributes(ctx context.Context, merchantID string) ([]importer.CanonicalAttribute, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT id, name, attribute_type, min_options, max_options
		 FROM configurable_attributes
		 WHERE merchant_id = ? AND enabled = TRUE
		 ORDER BY id ASC`,
		merchantID,
	)
	if err != nil {
		return nil, fmt.Errorf("import établissement: chargement des attributs: %w", err)
	}
	defer rows.Close()

	var attributes []importer.CanonicalAttribute
	for rows.Next() {
		var attribute importer.CanonicalAttribute
		if err := rows.Scan(&attribute.ExternalID, &attribute.Name, &attribute.Type,
			&attribute.MinOptions, &attribute.MaxOptions); err != nil {
			return nil, fmt.Errorf("import établissement: lecture d'un attribut: %w", err)
		}
		attributes = append(attributes, attribute)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range attributes {
		options, err := r.loadMerchantCanonicalAttributeOptions(ctx, attributes[i].ExternalID)
		if err != nil {
			return nil, err
		}
		attributes[i].Options = options
	}

	return attributes, nil
}

func (r *MenuRepository) loadMerchantCanonicalAttributeOptions(ctx context.Context, attributeID string) ([]importer.CanonicalOption, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT id, title, extra_price, component_id, quantity, unit_of_measure
		 FROM configurable_attribute_options
		 WHERE configurable_attribute_id = ? AND enabled = 1
		 ORDER BY id ASC`,
		attributeID,
	)
	if err != nil {
		return nil, fmt.Errorf("import établissement: chargement des options de %q: %w", attributeID, err)
	}
	defer rows.Close()

	var out []importer.CanonicalOption
	for rows.Next() {
		var (
			optionID      int
			title         string
			extraPrice    int
			componentID   sql.NullInt64
			quantity      sql.NullFloat64
			unitOfMeasure sql.NullInt64
		)
		if err := rows.Scan(&optionID, &title, &extraPrice, &componentID, &quantity, &unitOfMeasure); err != nil {
			return nil, fmt.Errorf("import établissement: lecture d'une option: %w", err)
		}

		option := importer.CanonicalOption{
			ExternalID: strconv.Itoa(optionID),
			Title:      title,
			ExtraPrice: extraPrice,
		}
		if componentID.Valid {
			option.ComponentExternalID = strconv.FormatInt(componentID.Int64, 10)
			if quantity.Valid {
				option.Quantity = quantity.Float64
			}
			if unitOfMeasure.Valid {
				option.UnitOfMeasureID = strconv.FormatInt(unitOfMeasure.Int64, 10)
			}
		}
		out = append(out, option)
	}
	return out, rows.Err()
}

// loadMerchantTvaRatesByID indexe la table globale tva_categories par tva_id.
// Global (pas de merchant_id) : les mêmes lignes valent pour tous les
// marchands, source comme destination — c'est ce qui permet d'extraire un
// taux depuis l'ID du marchand source et de le faire résoudre par le moteur
// existant (tvaResolver) contre la configuration du marchand destination, sans
// aucune modification de preview.go/commit_plan.go. Sans filtre enabled :
// un produit source peut porter un tva_id que sa propre caisse a depuis
// désactivé, le taux reste le même taux.
func (r *MenuRepository) loadMerchantTvaRatesByID(ctx context.Context) (map[int]float64, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `SELECT tva_id, tva_rate FROM tva_categories`)
	if err != nil {
		return nil, fmt.Errorf("import établissement: chargement des taux de TVA: %w", err)
	}
	defer rows.Close()

	out := make(map[int]float64)
	for rows.Next() {
		var (
			tvaID int
			rate  float64
		)
		if err := rows.Scan(&tvaID, &rate); err != nil {
			return nil, fmt.Errorf("import établissement: lecture d'un taux de TVA: %w", err)
		}
		out[tvaID] = rate
	}
	return out, rows.Err()
}

// loadMerchantCanonicalProducts lit les produits actifs du marchand source et
// leur composition (tags, attributs, recette), en trois requêtes scopées par
// marchand plutôt qu'en N+1 par produit — même discipline que
// LoadImportPreviewLookups, le pool ne tenant qu'une connexion ouverte.
func (r *MenuRepository) loadMerchantCanonicalProducts(
	ctx context.Context,
	merchantID string,
	categIDByMerchantCategID map[string]int,
	tvaRateByID map[int]float64,
) ([]importer.CanonicalProduct, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT product_id, name, product_desc, price, price_take_away, price_delivery,
		        category, tva_in_id, tva_delivery_id, tva_take_away_id,
		        available_in, available_take_away, available_delivery
		 FROM products
		 WHERE merchant_id = ? AND enabled = TRUE
		 ORDER BY product_id ASC`,
		merchantID,
	)
	if err != nil {
		return nil, fmt.Errorf("import établissement: chargement des produits: %w", err)
	}

	type rawProduct struct {
		productID                                         int
		name, description                                 string
		priceIn, priceTakeAway, priceDelivery             int
		merchantCategID                                   string
		tvaInID, tvaDeliveryID, tvaTakeAwayID             int
		availableIn, availableTakeAway, availableDelivery bool
	}

	var raw []rawProduct
	for rows.Next() {
		var (
			p           rawProduct
			description sql.NullString
		)
		if err := rows.Scan(&p.productID, &p.name, &description, &p.priceIn, &p.priceTakeAway, &p.priceDelivery,
			&p.merchantCategID, &p.tvaInID, &p.tvaDeliveryID, &p.tvaTakeAwayID,
			&p.availableIn, &p.availableTakeAway, &p.availableDelivery); err != nil {
			rows.Close()
			return nil, fmt.Errorf("import établissement: lecture d'un produit: %w", err)
		}
		p.description = description.String
		raw = append(raw, p)
	}
	closeErr := rows.Err()
	rows.Close()
	if closeErr != nil {
		return nil, closeErr
	}

	tagsByProduct, err := r.loadMerchantProductTags(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	attributesByProduct, err := r.loadMerchantProductAttributes(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	componentsByProduct, err := r.loadMerchantProductComponents(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	rateFor := func(tvaID int) *float64 {
		if tvaID == 0 {
			return nil
		}
		if rate, ok := tvaRateByID[tvaID]; ok {
			return &rate
		}
		return nil
	}

	out := make([]importer.CanonicalProduct, 0, len(raw))
	for _, p := range raw {
		externalID := strconv.Itoa(p.productID)

		product := importer.CanonicalProduct{
			ExternalID:           externalID,
			Name:                 p.name,
			Description:          p.description,
			PriceIn:              p.priceIn,
			PriceTakeAway:        p.priceTakeAway,
			PriceDelivery:        p.priceDelivery,
			TvaRateIn:            rateFor(p.tvaInID),
			TvaRateTakeAway:      rateFor(p.tvaTakeAwayID),
			TvaRateDelivery:      rateFor(p.tvaDeliveryID),
			TagExternalIDs:       tagsByProduct[externalID],
			AttributeExternalIDs: attributesByProduct[externalID],
			Components:           componentsByProduct[externalID],
			AllPricesZero:        p.priceIn == 0 && p.priceTakeAway == 0 && p.priceDelivery == 0,
		}
		if categExternalID, ok := categIDByMerchantCategID[p.merchantCategID]; ok {
			product.CategoryExternalID = strconv.Itoa(categExternalID)
		}
		availableIn, availableTakeAway, availableDelivery := p.availableIn, p.availableTakeAway, p.availableDelivery
		product.AvailableIn = &availableIn
		product.AvailableTakeAway = &availableTakeAway
		product.AvailableDelivery = &availableDelivery

		out = append(out, product)
	}

	return out, nil
}

// loadMerchantProductTags lit product_tags pour le marchand source.
// product_tags.product_id est un varchar sans lien déclaré vers products
// (integer) : on convertit products.product_id en texte (menuCastChar, déjà
// utilisé ailleurs dans ce fichier pour la même jointure) plutôt que
// product_tags.product_id en entier — pt.product_id peut porter n'importe
// quel texte, un CAST(... AS INTEGER) y échouerait durement sous Postgres.
func (r *MenuRepository) loadMerchantProductTags(ctx context.Context, merchantID string) (map[string][]string, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT pt.product_id, pt.tag_id
		 FROM product_tags pt
		 INNER JOIN products p ON pt.product_id = `+menuCastChar("p.product_id")+`
		 WHERE p.merchant_id = ? AND p.enabled = TRUE
		 ORDER BY pt.product_id ASC`,
		merchantID,
	)
	if err != nil {
		return nil, fmt.Errorf("import établissement: chargement des tags produit: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]string)
	for rows.Next() {
		var productID, tagID string
		if err := rows.Scan(&productID, &tagID); err != nil {
			return nil, fmt.Errorf("import établissement: lecture d'un tag produit: %w", err)
		}
		out[productID] = append(out[productID], tagID)
	}
	return out, rows.Err()
}

// loadMerchantProductAttributes lit product_configurable_attribute pour le
// marchand source. Même traitement de product_id que loadMerchantProductTags.
func (r *MenuRepository) loadMerchantProductAttributes(ctx context.Context, merchantID string) (map[string][]string, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT pca.product_id, pca.configurable_attribute_id
		 FROM product_configurable_attribute pca
		 INNER JOIN products p ON pca.product_id = `+menuCastChar("p.product_id")+`
		 WHERE p.merchant_id = ? AND p.enabled = TRUE AND pca.enabled = TRUE
		 ORDER BY pca.product_id ASC, pca.num_order ASC`,
		merchantID,
	)
	if err != nil {
		return nil, fmt.Errorf("import établissement: chargement des attributs produit: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]string)
	for rows.Next() {
		var productID, attributeID string
		if err := rows.Scan(&productID, &attributeID); err != nil {
			return nil, fmt.Errorf("import établissement: lecture d'un attribut produit: %w", err)
		}
		out[productID] = append(out[productID], attributeID)
	}
	return out, rows.Err()
}

func (r *MenuRepository) loadMerchantProductComponents(ctx context.Context, merchantID string) (map[string][]importer.CanonicalProductComponent, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT rc.product_id, rq.component_id, rq.quantity, rq.unit_of_measure,
		        rq.in_orders, rq.take_away_orders, rq.delivery_orders
		 FROM requires rq
		 INNER JOIN recipes rc ON rc.recipe_id = rq.recipe_id
		 INNER JOIN products p ON p.product_id = rc.product_id AND p.merchant_id = ?
		 WHERE p.enabled = TRUE AND rq.enabled = TRUE AND rq.component_id IS NOT NULL
		 ORDER BY rc.product_id ASC, rq.id ASC`,
		merchantID,
	)
	if err != nil {
		return nil, fmt.Errorf("import établissement: chargement des compositions: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]importer.CanonicalProductComponent)
	for rows.Next() {
		var (
			productID     int
			componentID   int
			quantity      float64
			unitOfMeasure int
			inOrders      bool
			takeAway      bool
			delivery      bool
		)
		if err := rows.Scan(&productID, &componentID, &quantity, &unitOfMeasure, &inOrders, &takeAway, &delivery); err != nil {
			return nil, fmt.Errorf("import établissement: lecture d'une ligne de composition: %w", err)
		}
		key := strconv.Itoa(productID)
		out[key] = append(out[key], importer.CanonicalProductComponent{
			ComponentExternalID: strconv.Itoa(componentID),
			Quantity:            quantity,
			UnitOfMeasureID:     strconv.Itoa(unitOfMeasure),
			InOrders:            inOrders,
			TakeAwayOrders:      takeAway,
			DeliveryOrders:      delivery,
		})
	}
	return out, rows.Err()
}
