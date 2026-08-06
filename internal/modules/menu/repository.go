package menu

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	redisclient "welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

type MenuRepository struct {
	database *sql.DB
	redis    *redisclient.Client
}

func NewMenuRepository(db *sql.DB, redis *redisclient.Client) *MenuRepository {
	return &MenuRepository{database: db, redis: redis}
}

// menuCastChar caste une expression en texte selon le dialecte — même pattern
// que orders.castChar (CAST AS CHAR sans longueur = char(1) en Postgres).
// Utilisé pour les jointures cross-type héritées (PK integer vs varchar).
func menuCastChar(expr string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "CAST(" + expr + " AS TEXT)"
	}
	return "CAST(" + expr + " AS CHAR)"
}

// menuNumericID reproduit la coercition MySQL non-strict d'un identifiant
// client lié à une colonne integer : toute chaîne non numérique valait 0
// (donc "aucune correspondance") — Postgres lèverait une erreur de type dure.
func menuNumericID(id string) bool {
	_, err := strconv.Atoi(strings.TrimSpace(id))
	return err == nil
}

// capitalizeFirst met la première lettre en majuscule en raisonnant sur la
// première *rune*, pas sur le premier octet. L'ancienne forme
// `strings.ToUpper(string(name[0])) + name[1:]` corrompait tout nom commençant
// par un caractère multi-octets : "épicerie" (0xC3 0xA9 ...) devenait
// "Ã©picerie", l'octet de tête étant réinterprété comme une rune isolée.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && size <= 1 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// AvailableProduct is a lightweight product record used for upsell candidate selection.
// It contains only the fields needed to build a SuggestedItem and to display the
// suggestion in the front-end — no TVA, no configuration tree.
type AvailableProduct struct {
	ProductID    string
	Name         string
	Price        int64
	CategoryID   string
	CategoryName string
	ImageURL     *string
	IsPopular    bool
}

// ListAvailableProductsForUpsell returns all orderable products for a merchant.
// Filters applied:
//   - p.available = 1 AND p.enabled = 1
//   - p.status IN ('available', '1')    (consistent with scannorder)
//   - root products only (no sub-products: by_product_of IS NULL)
//
// Ordered by category then name for deterministic slicing.
func (r *MenuRepository) ListAvailableProductsForUpsell(ctx context.Context, merchantID string) ([]AvailableProduct, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	rows, err := db.QueryContext(ctx, `
		SELECT
			p.product_id,
			p.name,
			p.price,
			COALESCE(p.category, '')   AS category_id,
			COALESCE(pc.categ_name, '') AS category_name,
			p.image_url,
			COALESCE(p.is_popular, FALSE)  AS is_popular
		FROM products p
		LEFT JOIN productcateg pc
			ON pc.merchant_categ_id = p.category
			AND pc.merchant_id = p.merchant_id
		WHERE p.merchant_id = ?
		  AND p.available = TRUE
		  AND p.enabled   = TRUE
		  AND p.status    IN ('available', '1')
		  AND (p.by_product_of IS NULL OR p.by_product_of = 0)
		ORDER BY category_id, p.name ASC
	`, merchantID)
	if err != nil {
		log.Error("upsell: ListAvailableProductsForUpsell query failed: " + err.Error())
		return nil, err
	}
	defer rows.Close()

	result := make([]AvailableProduct, 0)
	for rows.Next() {
		var ap AvailableProduct
		var imageURL sql.NullString
		var isPopular sql.NullBool
		if err := rows.Scan(
			&ap.ProductID,
			&ap.Name,
			&ap.Price,
			&ap.CategoryID,
			&ap.CategoryName,
			&imageURL,
			&isPopular,
		); err != nil {
			log.Error("upsell: ListAvailableProductsForUpsell scan failed: " + err.Error())
			return nil, err
		}
		if imageURL.Valid {
			ap.ImageURL = &imageURL.String
		}
		if isPopular.Valid {
			ap.IsPopular = isPopular.Bool
		}
		result = append(result, ap)
	}

	return result, rows.Err()
}

func (r *MenuRepository) GetUnitsOfMeasures(ctx context.Context, merchantID string) ([]Unit, error) {
	db := dbx.GetDB(ctx, r.database)

	// 1. Récupérer les unités et leurs descriptions (en français par défaut ici)
	unitsQuery := `
		SELECT ` + menuCastChar("u.id") + ` as id, d.uom_desc, COALESCE(d.uom_short_desc, '') as uom_short_desc
		FROM unit_of_measure u
		JOIN unit_of_measure_desc d ON u.id = d.id
		WHERE d.lang = 'FR'
		ORDER BY u.id`

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
		if err := rows.Scan(&u.ID, &u.Name, &u.ShortName); err != nil {
			return nil, err
		}
		u.Conversions = []UnitConversion{{
			ToUnitID:        u.ID,
			ToUnitName:      u.Name,
			ToUnitShortName: u.ShortName,
			Multiplier:      1,
		}}
		unitsMap[u.ID] = &u
		unitOrder = append(unitOrder, u.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 2. Récupérer les conversions dans un format directement exploitable par les applications.
	conversionQuery := `
		SELECT
			` + menuCastChar("conv.id_from") + `,
			` + menuCastChar("conv.id_to") + `,
			COALESCE(target.uom_desc, ''),
			COALESCE(target.uom_short_desc, ''),
			conv.ratio
		FROM unit_of_measure_convert conv
		JOIN unit_of_measure_desc target ON target.id = conv.id_to AND target.lang = 'FR'
		ORDER BY conv.id_from, conv.id_to`

	compatRows, err := db.QueryContext(ctx, conversionQuery)
	if err != nil {
		return nil, err
	}
	defer compatRows.Close()

	for compatRows.Next() {
		var from, to, toName, toShortName string
		var ratio float64
		if err := compatRows.Scan(&from, &to, &toName, &toShortName, &ratio); err != nil {
			return nil, err
		}
		if ratio == 0 {
			return nil, fmt.Errorf("invalid unit conversion ratio for %s -> %s", from, to)
		}

		// En base, ratio convertit de l'unité cible vers l'unité source.
		// L'API expose donc un multiplicateur direct: source * multiplier = cible.
		if unit, ok := unitsMap[from]; ok {
			unit.Conversions = append(unit.Conversions, UnitConversion{
				ToUnitID:        to,
				ToUnitName:      toName,
				ToUnitShortName: toShortName,
				Multiplier:      1 / ratio,
			})
		}
	}
	if err := compatRows.Err(); err != nil {
		return nil, err
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
	db := dbx.GetDB(ctx, r.database)

	// 1. Récupération des attributs
	// brand filtre les attributs créés par les plateformes tierces (UBER_EATS,
	// DELIVEROO) : seuls les attributs WELLO_RESTO sont exposés par cette route.
	attrQuery := `
        SELECT id, attribute_type, name, title, min_options, max_options
        FROM configurable_attributes
        WHERE merchant_id = ? AND enabled = TRUE AND brand = ?`

	attrRows, err := db.QueryContext(ctx, attrQuery, merchantID, models.BrandWelloResto)
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
	// ca.enabled est boolean en cible, cao.enabled est resté integer (0/1)
	optQuery := `
        SELECT cao.id, cao.configurable_attribute_id, cao.title, cao.max_quantity, cao.extra_price, cao.enabled, cao.image_url,
               cao.component_id, cao.quantity, cao.unit_of_measure
        FROM configurable_attributes ca
        INNER JOIN configurable_attribute_options cao ON cao.configurable_attribute_id = ca.id
        WHERE ca.merchant_id = ? AND ca.enabled = TRUE AND ca.brand = ? AND cao.enabled = 1`

	optRows, err := db.QueryContext(ctx, optQuery, merchantID, models.BrandWelloResto)
	if err != nil {
		return nil, fmt.Errorf("query options failed: %w", err)
	}
	defer optRows.Close()

	for optRows.Next() {
		var opt AttributeOption
		var parentAttrID string
		var componentID sql.NullInt64
		var quantity sql.NullFloat64
		var unitOfMeasure sql.NullInt64

		if err := optRows.Scan(&opt.ID, &parentAttrID, &opt.Title, &opt.MaxQuantity, &opt.Price, &opt.Enabled, &opt.ImageURL,
			&componentID, &quantity, &unitOfMeasure); err != nil {
			return nil, fmt.Errorf("scan option failed: %w", err)
		}
		if componentID.Valid {
			cid := strconv.FormatInt(componentID.Int64, 10)
			opt.ComponentID = &cid
		}
		if quantity.Valid {
			q := quantity.Float64
			opt.Quantity = &q
		}
		if unitOfMeasure.Valid {
			uom := strconv.FormatInt(unitOfMeasure.Int64, 10)
			opt.UnitOfMeasureID = &uom
		}

		// 3. Mapping Magique : on trouve l'attribut parent instantanément grâce à la map
		if index, exists := attrIndexMap[parentAttrID]; exists {
			attributes[index].Options = append(attributes[index].Options, opt)
		}
	}

	if err := optRows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error (options): %w", err)
	}

	// 3. Comptage du nombre de produits liés à chaque attribut
	// GROUP BY sur configurable_attribute_id, colonne de tête de la PK (configurable_attribute_id, product_id)
	// -> scan d'index, pas de coût supplémentaire notable même sur un gros catalogue.
	countQuery := `
        SELECT pca.configurable_attribute_id, COUNT(*)
        FROM product_configurable_attribute pca
        INNER JOIN configurable_attributes ca ON ca.id = pca.configurable_attribute_id
        WHERE ca.merchant_id = ? AND ca.enabled = TRUE AND ca.brand = ? AND pca.enabled = TRUE
        GROUP BY pca.configurable_attribute_id`

	countRows, err := db.QueryContext(ctx, countQuery, merchantID, models.BrandWelloResto)
	if err != nil {
		return nil, fmt.Errorf("query attribute product counts failed: %w", err)
	}
	defer countRows.Close()

	for countRows.Next() {
		var attrID string
		var count int
		if err := countRows.Scan(&attrID, &count); err != nil {
			return nil, fmt.Errorf("scan attribute product count failed: %w", err)
		}

		if index, exists := attrIndexMap[attrID]; exists {
			attributes[index].ProductCount = count
		}
	}

	if err := countRows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error (attribute product counts): %w", err)
	}

	// Si aucun attribut n'est trouvé, on renvoie une slice vide et pas nil
	if attributes == nil {
		attributes = []Attribute{}
	}

	return attributes, nil
}

func (r *MenuRepository) GetAttribute(ctx context.Context, merchantID, attributeID string) (*Attribute, error) {
	db := dbx.GetDB(ctx, r.database)

	// 1. Récupération de l'attribut
	attrQuery := `
        SELECT id, attribute_type, name, title, min_options, max_options
        FROM configurable_attributes
        WHERE id = ? AND merchant_id = ? AND enabled = TRUE`

	var attr Attribute
	err := db.QueryRowContext(ctx, attrQuery, attributeID, merchantID).Scan(
		&attr.ID, &attr.Type, &attr.Name, &attr.Title, &attr.Min, &attr.Max)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("attribute_not_found")
		}
		return nil, fmt.Errorf("query attribute failed: %w", err)
	}

	// Initialisation à vide pour éviter le "null" en JSON si l'attribut n'a pas d'options
	attr.Options = []AttributeOption{}

	// 2. Récupération des options (enabled est resté integer sur cette table)
	optQuery := `
        SELECT id, configurable_attribute_id, title, max_quantity, extra_price, enabled, image_url,
               component_id, quantity, unit_of_measure
        FROM configurable_attribute_options
        WHERE configurable_attribute_id = ? AND enabled = 1`

	optRows, err := db.QueryContext(ctx, optQuery, attributeID)
	if err != nil {
		return nil, fmt.Errorf("query options failed: %w", err)
	}
	defer optRows.Close()

	for optRows.Next() {
		var opt AttributeOption
		var parentAttrID string
		var componentID sql.NullInt64
		var quantity sql.NullFloat64
		var unitOfMeasure sql.NullInt64

		if err := optRows.Scan(&opt.ID, &parentAttrID, &opt.Title, &opt.MaxQuantity, &opt.Price, &opt.Enabled, &opt.ImageURL,
			&componentID, &quantity, &unitOfMeasure); err != nil {
			return nil, fmt.Errorf("scan option failed: %w", err)
		}
		if componentID.Valid {
			cid := strconv.FormatInt(componentID.Int64, 10)
			opt.ComponentID = &cid
		}
		if quantity.Valid {
			q := quantity.Float64
			opt.Quantity = &q
		}
		if unitOfMeasure.Valid {
			uom := strconv.FormatInt(unitOfMeasure.Int64, 10)
			opt.UnitOfMeasureID = &uom
		}

		attr.Options = append(attr.Options, opt)
	}

	if err := optRows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error (options): %w", err)
	}

	return &attr, nil
}

// CreateAttribute est l'enveloppe unitaire du chemin POST /menu/attributes :
// contrôle d'unicité du nom (avec sa confirmation Redis), puis insertion.
// Le cœur d'insertion réutilisable est insertAttributeTx, volontairement
// dépourvu de tout contrôle d'unicité.
//
// TODO: non atomique — attribut + options partiellement commités si une option
// échoue ; à traiter dans un ticket dédié, hors refactor.
func (r *MenuRepository) CreateAttribute(ctx context.Context, merchantID string, payload *UpdateAttributePayload) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	// Vérifier qu'aucun attribut actif ne porte déjà ce nom
	var attributeNameExists int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM configurable_attributes WHERE merchant_id = ? AND LOWER(name) = LOWER(?) AND enabled = TRUE`,
		merchantID, strings.TrimSpace(payload.Name),
	).Scan(&attributeNameExists)
	if err != nil {
		return "", fmt.Errorf("failed to check attribute name uniqueness: %w", err)
	}
	if attributeNameExists > 0 {
		if r.redis == nil {
			return "", models.ErrAttributeNameAlreadyExists
		}
		// Redis actif : le 1er appel pose une clé de confirmation et bloque ;
		// un 2e appel identique (même merchant + même nom) la trouve déjà
		// posée et la création est acceptée malgré le doublon.
		confirmKey := helpers.GetMenuAttributeNameConfirmKey(merchantID, payload.Name)
		if r.redis.SetNX(ctx, confirmKey, "1", models.MenuNameConfirmTTL) {
			return "", models.ErrAttributeNameAlreadyExistsWithRetry
		}
		r.redis.Delete(ctx, confirmKey)
	}

	return r.insertAttributeTx(ctx, merchantID, payload)
}

// insertAttributeTx insère un attribut configurable et ses options, sans aucun
// contrôle d'unicité de nom et sans ouvrir de transaction propre : il s'exécute
// dans celle portée par ctx s'il y en a une (dbx.GetDB), sinon en autocommit —
// exactement le comportement de l'appel unitaire. Un appelant en lot peut donc
// l'envelopper dans son propre RunInTx pour obtenir l'atomicité.
func (r *MenuRepository) insertAttributeTx(ctx context.Context, merchantID string, payload *UpdateAttributePayload) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	// Generate new UUID for attribute
	attributeID := helpers.GeneratePrefixedID(helpers.AttributeIDPrefix)

	// Insert the new attribute.
	// product_id est NOT NULL sans défaut et n'était jamais renseigné (MySQL
	// non-strict insérait 0 — colonne héritée, le lien produit passe par
	// product_configurable_attribute) : 0 explicite pour la parité Postgres.
	insertAttrQuery := `
		INSERT INTO configurable_attributes (
			id,
			merchant_id,
			product_id,
			attribute_type,
			name,
			title,
			min_options,
			max_options,
			enabled
		) VALUES (?, ?, 0, ?, ?, ?, ?, ?, TRUE)
	`

	_, err := db.ExecContext(ctx, insertAttrQuery,
		attributeID,
		merchantID,
		payload.Type,
		payload.Name,
		payload.Title,
		payload.Min,
		payload.Max)
	if err != nil {
		return "", fmt.Errorf("insert attribute error: %w", err)
	}

	if err := r.insertAttributeOptionsTx(ctx, attributeID, payload.Options); err != nil {
		return "", err
	}

	return attributeID, nil
}

// insertAttributeOptionsTx insère les options d'un attribut qui vient d'être
// créé. Même contrat transactionnel que insertAttributeTx. Sert uniquement le
// chemin de création : la branche INSERT de UpdateAttribute, dont le corps est
// identique, reste volontairement autonome (dedup hors périmètre du refactor).
func (r *MenuRepository) insertAttributeOptionsTx(ctx context.Context, attributeID string, options []UpdateAttributeOptionPayload) error {
	db := dbx.GetDB(ctx, r.database)

	// Process options
	for _, opt := range options {
		price := opt.Price
		if opt.ExtraPrice != nil {
			price = *opt.ExtraPrice
		}

		maxQty := 1
		if opt.MaxQuantity != nil {
			maxQty = *opt.MaxQuantity
		}

		enabled := true
		if opt.Enabled != nil {
			enabled = *opt.Enabled
		}

		// Lien ingrédient défensif : jamais de quantity/unit_of_measure
		// orphelins sans component_id valide, quoi que le client envoie.
		var componentIDArg, quantityArg, unitArg interface{}
		if opt.ComponentID != "" && menuNumericID(opt.ComponentID) {
			componentIDArg = opt.ComponentID
			quantityArg = opt.Quantity
			if opt.UnitOfMeasureID != "" && menuNumericID(opt.UnitOfMeasureID) {
				unitArg = opt.UnitOfMeasureID
			}
		}

		// id est une colonne auto-incrémentée (identity en cible) : l'ancien
		// ID préfixé généré côté client était silencieusement coercé à 0 par
		// MySQL, qui générait alors sa propre valeur — on laisse la base
		// générer l'id dans les deux dialectes (même effet net).
		// enabled est resté integer (0/1) sur cette table -> conversion Go.
		insertOptQuery := `
			INSERT INTO configurable_attribute_options (
				configurable_attribute_id,
				title,
				extra_price,
				max_quantity,
				enabled,
				image_url,
				component_id,
				quantity,
				unit_of_measure
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`

		enabledInt := 0
		if enabled {
			enabledInt = 1
		}
		_, err := db.ExecContext(ctx, insertOptQuery,
			attributeID,
			opt.Title,
			price,
			maxQty,
			enabledInt,
			opt.ImageURL,
			componentIDArg,
			quantityArg,
			unitArg)
		if err != nil {
			return fmt.Errorf("insert option error: %w", err)
		}
	}

	return nil
}

func (r *MenuRepository) UpdateAttribute(ctx context.Context, merchantID, attributeID string, payload *UpdateAttributePayload) error {
	db := dbx.GetDB(ctx, r.database)

	// 1. Verify attribute exists and belongs to merchant
	var existsCheck int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM configurable_attributes WHERE id = ? AND merchant_id = ?`,
		attributeID, merchantID).Scan(&existsCheck)
	if err != nil || existsCheck == 0 {
		return fmt.Errorf("attribute_not_found")
	}

	// 2. Update the attribute itself
	updateAttrQuery := `
		UPDATE configurable_attributes
		SET 
			attribute_type = ?,
			name = ?,
			title = ?,
			min_options = ?,
			max_options = ?
		WHERE id = ? AND merchant_id = ?
	`

	_, err = db.ExecContext(ctx, updateAttrQuery,
		payload.Type,
		payload.Name,
		payload.Title,
		payload.Min,
		payload.Max,
		attributeID,
		merchantID)
	if err != nil {
		return fmt.Errorf("update attribute error: %w", err)
	}

	// 2.5. Disable all existing options for this attribute (before processing new ones)
	_, err = db.ExecContext(ctx,
		`UPDATE configurable_attribute_options SET enabled = 0 WHERE configurable_attribute_id = ?`,
		attributeID)
	if err != nil {
		return fmt.Errorf("disable options error: %w", err)
	}

	// 3. Process options
	for _, opt := range payload.Options {
		if opt.ID != nil && *opt.ID != "" {
			// Option exists - update it
			price := opt.Price
			if opt.ExtraPrice != nil {
				price = *opt.ExtraPrice
			}

			maxQty := 1
			if opt.MaxQuantity != nil {
				maxQty = *opt.MaxQuantity
			}

			enabled := true
			if opt.Enabled != nil {
				enabled = *opt.Enabled
			}

			// Lien ingrédient défensif : jamais de quantity/unit_of_measure
			// orphelins sans component_id valide, quoi que le client envoie.
			var componentIDArg, quantityArg, unitArg interface{}
			if opt.ComponentID != "" && menuNumericID(opt.ComponentID) {
				componentIDArg = opt.ComponentID
				quantityArg = opt.Quantity
				if opt.UnitOfMeasureID != "" && menuNumericID(opt.UnitOfMeasureID) {
					unitArg = opt.UnitOfMeasureID
				}
			}

			// image_url passe par COALESCE : si le payload ne le fournit pas
			// (cas du formulaire back-office actuel, qui ne connaît pas ce
			// champ), l'image déjà uploadée via le endpoint dédié est
			// préservée plutôt qu'écrasée à NULL à chaque sauvegarde de
			// l'attribut. component_id/quantity/unit_of_measure n'utilisent
			// PAS ce COALESCE : le formulaire les envoie systématiquement
			// (valeurs vides comprises), un écrasement direct est donc ce qui
			// permet d'effacer un lien ingrédient déjà posé.
			updateOptQuery := `
				UPDATE configurable_attribute_options
				SET
					title = ?,
					extra_price = ?,
					max_quantity = ?,
					enabled = ?,
					image_url = COALESCE(?, image_url),
					component_id = ?,
					quantity = ?,
					unit_of_measure = ?
				WHERE id = ? AND configurable_attribute_id = ?
			`

			// enabled est resté integer (0/1) sur cette table ; id est un
			// integer identity — un *opt.ID non numérique valait 0 en MySQL
			// (aucune ligne), on reproduit ce comportement côté Go.
			enabledInt := 0
			if enabled {
				enabledInt = 1
			}
			optID := *opt.ID
			if !menuNumericID(optID) {
				optID = "0"
			}
			_, err = db.ExecContext(ctx, updateOptQuery,
				opt.Title,
				price,
				maxQty,
				enabledInt,
				opt.ImageURL,
				componentIDArg,
				quantityArg,
				unitArg,
				optID,
				attributeID)
			if err != nil {
				return fmt.Errorf("update option error: %w", err)
			}
		} else {
			// New option - create it
			price := opt.Price
			if opt.ExtraPrice != nil {
				price = *opt.ExtraPrice
			}

			maxQty := 1
			if opt.MaxQuantity != nil {
				maxQty = *opt.MaxQuantity
			}

			enabled := true
			if opt.Enabled != nil {
				enabled = *opt.Enabled
			}

			// Lien ingrédient défensif : jamais de quantity/unit_of_measure
			// orphelins sans component_id valide, quoi que le client envoie.
			var componentIDArg, quantityArg, unitArg interface{}
			if opt.ComponentID != "" && menuNumericID(opt.ComponentID) {
				componentIDArg = opt.ComponentID
				quantityArg = opt.Quantity
				if opt.UnitOfMeasureID != "" && menuNumericID(opt.UnitOfMeasureID) {
					unitArg = opt.UnitOfMeasureID
				}
			}

			insertOptQuery := `
				INSERT INTO configurable_attribute_options (
					configurable_attribute_id,
					title,
					extra_price,
					max_quantity,
					enabled,
					image_url,
					component_id,
					quantity,
					unit_of_measure
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			`

			enabledInt := 0
			if enabled {
				enabledInt = 1
			}
			_, err = db.ExecContext(ctx, insertOptQuery,
				attributeID,
				opt.Title,
				price,
				maxQty,
				enabledInt,
				opt.ImageURL,
				componentIDArg,
				quantityArg,
				unitArg)
			if err != nil {
				return fmt.Errorf("insert option error: %w", err)
			}
		}
	}

	return nil
}

// GetAttributeOptionImageURL récupère l'URL d'image actuelle d'une option,
// scopée au merchant via une jointure sur configurable_attributes (les
// options n'ont pas de merchant_id direct).
func (r *MenuRepository) GetAttributeOptionImageURL(ctx context.Context, merchantID, optionID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	// cao.id est un integer identity : un optionID non numérique valait 0 en
	// MySQL (aucune ligne) — même résultat sans requête.
	if !menuNumericID(optionID) {
		return "", nil
	}

	var imageURL sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT cao.image_url
		 FROM configurable_attribute_options cao
		 INNER JOIN configurable_attributes ca ON ca.id = cao.configurable_attribute_id
		 WHERE cao.id = ? AND ca.merchant_id = ?`,
		optionID, merchantID,
	).Scan(&imageURL)

	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("failed to get attribute option image: %w", err)
	}

	if imageURL.Valid {
		return imageURL.String, nil
	}
	return "", nil
}

// UpdateAttributeOptionImageURL met à jour l'URL d'image d'une option,
// scopée au merchant via une jointure sur configurable_attributes.
func (r *MenuRepository) UpdateAttributeOptionImageURL(ctx context.Context, merchantID, optionID, imageURL string) error {
	db := dbx.GetDB(ctx, r.database)

	// cao.id est un integer identity : un optionID non numérique valait 0 en
	// MySQL (aucune ligne affectée) — même résultat sans requête.
	if !menuNumericID(optionID) {
		return fmt.Errorf("attribute_option_not_found")
	}

	// UPDATE multi-table MySQL -> UPDATE ... FROM (cible SET non qualifiée)
	query := `UPDATE configurable_attribute_options cao
		 INNER JOIN configurable_attributes ca ON ca.id = cao.configurable_attribute_id
		 SET cao.image_url = ?
		 WHERE cao.id = ? AND ca.merchant_id = ?`
	if dbx.ActiveDialect() == dbx.Postgres {
		query = `UPDATE configurable_attribute_options
		 SET image_url = ?
		 FROM configurable_attributes ca
		 WHERE ca.id = configurable_attribute_options.configurable_attribute_id
		   AND configurable_attribute_options.id = ? AND ca.merchant_id = ?`
	}

	res, err := db.ExecContext(ctx, query, imageURL, optionID, merchantID)
	if err != nil {
		return fmt.Errorf("failed to update attribute option image: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check update result: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("attribute_option_not_found")
	}

	return nil
}

func (r *MenuRepository) DeleteAttribute(ctx context.Context, merchantID, attributeID string) error {
	db := dbx.GetDB(ctx, r.database)

	// Verify attribute exists and belongs to merchant
	var existsCheck int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM configurable_attributes WHERE id = ? AND merchant_id = ?`,
		attributeID, merchantID).Scan(&existsCheck)
	if err != nil || existsCheck == 0 {
		return fmt.Errorf("attribute_not_found")
	}

	// Disable the attribute by setting enabled = FALSE
	_, err = db.ExecContext(ctx,
		`UPDATE configurable_attributes SET enabled = FALSE WHERE id = ? AND merchant_id = ?`,
		attributeID, merchantID)
	if err != nil {
		return fmt.Errorf("delete attribute error: %w", err)
	}

	return nil
}

func (r *MenuRepository) GetMenu(ctx context.Context, merchantID string, lastMenu *time.Time) (*models.MenuResponse, error) {
	db := dbx.GetDB(ctx, r.database)

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
		Image sql.NullString
	}
	{
		step := "categories"
		q := `
            SELECT pc.merchant_categ_id, pc.categ_name, pc.categ_order, pc.bg_color, pc.image_url
            FROM productcateg pc
            WHERE pc.available = TRUE AND pc.enabled = TRUE AND pc.merchant_id = ?
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
				Image sql.NullString
			}
			if err := rows.Scan(&c.ID, &c.Name, &c.Order, &c.Bg, &c.Image); err != nil {
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
                   p.sync_uber_eats, p.sync_deliveroo, p.display_order
            FROM products p
            INNER JOIN tva_categories tva_in on tva_in.tva_id = p.tva_in_id
            INNER JOIN tva_categories tva_delivery on tva_delivery.tva_id = p.tva_delivery_id
            INNER JOIN tva_categories tva_take_away on tva_take_away.tva_id = p.tva_take_away_id
            LEFT JOIN products subp on subp.product_id = p.by_product_of
            WHERE p.merchant_id = ?
			AND (subp.product_id IS NULL OR subp.product_id = p.product_id) AND p.status not in ('removed_from_menu') AND p.enabled = TRUE
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
				&availIn, &availTake, &availDel, &hasImage, &syncUberEats, &syncDeliveroo, &p.DisplayOrder,
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
				p.AvailableIn = &availIn.Bool
			}
			if availTake.Valid {
				p.AvailableTakeAway = &availTake.Bool
			}
			if availDel.Valid {
				p.AvailableDelivery = &availDel.Bool
			}
			if syncDeliveroo.Valid {
				p.SyncDeliveroo = &syncDeliveroo.Bool
			}
			if syncUberEats.Valid {
				p.SyncUberEats = &syncUberEats.Bool
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
                   p.available_in, p.available_take_away, p.available_delivery,
                   tva_in.tva_rate as tva_rate_in, tva_delivery.tva_rate as tva_rate_delivery, tva_take_away.tva_rate as tva_rate_take_away, p.bg_color, p.is_product_group, p.is_available_on_sno, p.status,
				   p.display_order
            FROM products p
            INNER JOIN tva_categories tva_in on tva_in.tva_id = p.tva_in_id
            INNER JOIN tva_categories tva_delivery on tva_delivery.tva_id = p.tva_delivery_id
            INNER JOIN tva_categories tva_take_away on tva_take_away.tva_id = p.tva_take_away_id
            WHERE p.merchant_id = ? AND p.by_product_of IS NOT NULL AND p.status not in ('removed_from_menu') AND p.enabled = TRUE
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
			if err := rows.Scan(&p.ProductID, &by, &p.Name, &p.Category, &p.CategoryID, &p.Price, &p.PriceTakeAway,
				&p.PriceDelivery, &desc, &availIn, &availTake, &availDel, &tvaIn, &tvaDel, &tvaTake, &bg, &p.IsProductGroup,
				&p.IsAvailableOnSNO, &p.Status, &p.DisplayOrder); err != nil {
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
				p.AvailableIn = &availIn.Bool
			}
			if availTake.Valid {
				p.AvailableTakeAway = &availTake.Bool
			}
			if availDel.Valid {
				p.AvailableDelivery = &availDel.Bool
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
            WHERE c.merchant_id = ? AND c.available = TRUE AND rq.enabled = true
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
			var uom, status sql.NullString
			if err := rows.Scan(&productID, &c.ComponentID, &c.Name, &c.Price, &status, &c.Quantity, &uom); err != nil {
				return nil, err
			}
			if uom.Valid {
				c.UnitOfMeasure = uom.String
			}
			c.Status = status.String
			compMap[productID] = append(compMap[productID], c)
			count++
		}
	}

	// --- STEP 6: configurable attributes + options (we load options then attrs like PHP) ---
	optMap := make(map[string][]models.ConfigurableOption)
	{
		step := "configurable_options"
		q := `
            SELECT DISTINCT ca.id as configurable_attribute_id, cao.id, cao.title, cao.extra_price, cao.max_quantity, cao.image_url
            FROM products p
            INNER JOIN product_configurable_attribute pca on pca.product_id = ` + menuCastChar("p.product_id") + `
            INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
            INNER JOIN configurable_attribute_options cao on cao.configurable_attribute_id = ca.id
            WHERE p.merchant_id = ? AND ca.enabled = TRUE AND cao.enabled = 1 AND pca.enabled = TRUE
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
			if err := rows.Scan(&cfgID, &o.ID, &o.Title, &o.ExtraPrice, &o.MaxQuantity, &o.ImageURL); err != nil {
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
            INNER JOIN product_configurable_attribute pca on pca.product_id = ` + menuCastChar("p.product_id") + `
            INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
            WHERE p.merchant_id = ? AND ca.enabled = TRUE AND pca.enabled = TRUE
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
				SELECT ` + menuCastChar("product_id") + ` FROM products WHERE merchant_id = ? AND available = TRUE AND enabled = TRUE
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
			SELECT pt.product_id, t.tag_id, t.name, COALESCE(t.display_order, 0) as display_order, t.color
			FROM product_tags pt
			INNER JOIN tags t ON t.tag_id = pt.tag_id
			WHERE t.merchant_id = ? AND pt.product_id IN (
				SELECT ` + menuCastChar("product_id") + ` FROM products WHERE merchant_id = ? AND available = TRUE AND enabled = TRUE
			)
			ORDER BY t.display_order ASC
		`
		rows, err := runQuery("tags_per_product", q, merchantID, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var productID string
			var t models.TagEntry
			if err := rows.Scan(&productID, &t.ID, &t.Name, &t.DisplayOrder, &t.Color); err != nil {
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
		q := `SELECT merchant_categ_id, name, categ_order FROM component_category WHERE merchant_id = ? AND available = TRUE ORDER BY categ_order ASC`
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
		ID                      string
		Name                    string
		CatID                   *string
		Status                  string
		Price                   int
		UnitOfMeasureID         int
		UnitOfMeasure           sql.NullString
		PurchasePrice           sql.NullInt64
		PurchasePriceQty        sql.NullFloat64
		PurchaseUnitOfMeasureID sql.NullInt64
		PurchaseUnitOfMeasure   sql.NullString
	}
	var allComponents []compBasicTmp
	{
		step := "all_components"
		q := `
		SELECT component_id, name, category_id, status, component_price, unit_of_measure, COALESCE(uomd.uom_desc, '') as uom_desc,
		purchase_price, purchase_price_quantity, c.purchase_unit_id, COALESCE(puomd.uom_desc, '') as purchase_uom_desc
		FROM components c
		LEFT JOIN unit_of_measure_desc uomd ON uomd.lang = 'FR' AND uomd.id = c.unit_of_measure
		LEFT JOIN unit_of_measure_desc puomd ON puomd.lang = 'FR' AND ` + menuCastChar("puomd.id") + ` = c.purchase_unit_id
		WHERE c.merchant_id = ? AND c.enabled = TRUE AND c.available = TRUE
		`
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var cb compBasicTmp
			if err := rows.Scan(&cb.ID, &cb.Name, &cb.CatID, &cb.Status, &cb.Price, &cb.UnitOfMeasureID, &cb.UnitOfMeasure, &cb.PurchasePrice, &cb.PurchasePriceQty, &cb.PurchaseUnitOfMeasureID, &cb.PurchaseUnitOfMeasure); err != nil {
				return nil, err
			}
			allComponents = append(allComponents, cb)
			count++
		}
	}

	// --- BUILD: attach sub-products to parents & attach components & configuration like PHP ---
	// enrich root products and sub-products before attachment so nested entries include same details
	for _, bucket := range []map[string]*models.ProductEntry{products, subProducts} {
		for _, p := range bucket {
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
	}

	// attach subproducts
	for _, sp := range subProducts {
		if sp.ByProductOf != nil {
			if parent, ok := products[*sp.ByProductOf]; ok && parent != nil {
				parent.SubProducts = append(parent.SubProducts, *sp)
			}
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
		var categoryImageURL *string
		if c.Image.Valid {
			categoryImageURL = &c.Image.String
		}
		productTypes = append(productTypes, models.ProductCategory{
			Category:     c.Name,
			CategoryName: c.Name,
			CategoryID:   c.ID,
			Order:        c.Order,
			BgColor:      bg,
			ImageURL:     categoryImageURL,
			Products:     actual,
		})
	}

	// --- build component types ---
	compTypes := []models.ComponentCategory{}
	for _, cc := range compCats {
		actual := []models.ComponentBasic{}
		for _, cb := range allComponents {
			if cb.CatID != nil && cc.ID != nil && *cb.CatID == *cc.ID {
				// Convertir les valeurs d'achat nullables en pointeurs
				var purchasePrice *int
				if cb.PurchasePrice.Valid {
					pp := int(cb.PurchasePrice.Int64)
					purchasePrice = &pp
				}

				var purchasePriceQty *float64
				if cb.PurchasePriceQty.Valid {
					ppq := float64(cb.PurchasePriceQty.Float64)
					purchasePriceQty = &ppq
				}

				uomID := fmt.Sprintf("%d", cb.UnitOfMeasureID)
				uomName := ""
				if cb.UnitOfMeasure.Valid {
					uomName = cb.UnitOfMeasure.String
				}

				purchaseUomID := ""
				if cb.PurchaseUnitOfMeasureID.Valid {
					purchaseUomID = fmt.Sprintf("%d", cb.PurchaseUnitOfMeasureID.Int64)
				}
				purchaseUomName := ""
				if cb.PurchaseUnitOfMeasure.Valid {
					purchaseUomName = cb.PurchaseUnitOfMeasure.String
				}

				actual = append(actual, models.ComponentBasic{
					ComponentID:             cb.ID,
					Name:                    cb.Name,
					Category:                cb.CatID,
					Price:                   cb.Price,
					Status:                  cb.Status,
					UnitOfMeasureID:         uomID,
					UnitOfMeasure:           uomName,
					PurchasePrice:           purchasePrice,
					PurchasePriceQty:        purchasePriceQty,
					PurchaseUnitOfMeasureID: purchaseUomID,
					PurchaseUnitOfMeasure:   purchaseUomName,
				})
			}
		}
		compTypes = append(compTypes, models.ComponentCategory{
			CategoryName: cc.Name,
			CategoryID:   *cc.ID,
			Order:        cc.Order,
			Components:   actual,
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

func (r *MenuRepository) GetAllProducts(ctx context.Context, merchantID string) ([]models.ProductCategory, error) {
	db := dbx.GetDB(ctx, r.database)

	// --- HELPER FUNCTIONS (same as GetMenu) ---
	runQuery := func(step string, query string, args ...interface{}) (*sql.Rows, error) {
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("%s query error: %w", step, err)
		}
		return rows, nil
	}

	// --- STEP 1: categories (NO available/enabled filter) ---
	var cats []struct {
		ID        *string
		Name      string
		Order     int
		Bg        sql.NullString
		Image     sql.NullString
		Available bool
	}
	{
		step := "categories_all_products"
		q := `
            SELECT pc.merchant_categ_id, pc.categ_name, pc.categ_order, pc.bg_color, pc.image_url, pc.available
            FROM productcateg pc
            WHERE pc.merchant_id = ?
			AND pc.enabled = TRUE
            ORDER BY pc.categ_order ASC
        `
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var c struct {
				ID        *string
				Name      string
				Order     int
				Bg        sql.NullString
				Image     sql.NullString
				Available bool
			}
			if err := rows.Scan(&c.ID, &c.Name, &c.Order, &c.Bg, &c.Image, &c.Available); err != nil {
				return nil, err
			}
			cats = append(cats, c)
		}
	}

	// --- STEP 2: products (roots, NO available/enabled filter) ---
	products := make(map[string]*models.ProductEntry)
	var productOrder []string
	{
		step := "products_roots_all"
		q := `
            SELECT p.product_id, p.by_product_of, p.name, p.category, pc.categ_name, p.price, p.price_take_away, p.price_delivery, p.price_uber_eats, p.price_deliveroo, p.product_desc,
                   tva_in.tva_rate as tva_rate_in, tva_delivery.tva_rate as tva_rate_delivery, tva_take_away.tva_rate as tva_rate_take_away,
                   p.bg_color, p.is_product_group, p.status, p.is_available_on_sno, p.is_popular, p.image_url, p.available_in, p.available_take_away, p.available_delivery,
                   CASE WHEN p.img IS NULL OR p.img = '' THEN false ELSE true END as has_image,
                   p.sync_uber_eats, p.sync_deliveroo, p.available, p.display_order
            FROM products p
			INNER JOIN productcateg pc on pc.merchant_categ_id = p.category and pc.merchant_id = p.merchant_id
            INNER JOIN tva_categories tva_in on tva_in.tva_id = p.tva_in_id
            INNER JOIN tva_categories tva_delivery on tva_delivery.tva_id = p.tva_delivery_id
            INNER JOIN tva_categories tva_take_away on tva_take_away.tva_id = p.tva_take_away_id
            LEFT JOIN products subp on subp.product_id = p.by_product_of
            WHERE p.merchant_id = ? AND (subp.product_id IS NULL OR subp.product_id = p.product_id)
			AND p.enabled = TRUE
        `
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
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
				&p.ProductID, &p.ByProductOf, &p.Name, &p.CategoryID, &p.CategoryName, &p.Price, &p.PriceTakeAway, &p.PriceDelivery, &p.PriceUberEats, &p.PriceDeliveroo,
				&desc, &tvaIn, &tvaDel, &tvaTake, &bg, &p.IsProductGroup, &p.Status, &p.IsAvailableOnSNO, &isPopular, &imageURL,
				&availIn, &availTake, &availDel, &hasImage, &syncUberEats, &syncDeliveroo, &p.Available, &p.DisplayOrder,
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
				p.AvailableIn = &availIn.Bool
			}
			if availTake.Valid {
				p.AvailableTakeAway = &availTake.Bool
			}
			if availDel.Valid {
				p.AvailableDelivery = &availDel.Bool
			}
			if syncDeliveroo.Valid {
				p.SyncDeliveroo = &syncDeliveroo.Bool
			}
			if syncUberEats.Valid {
				p.SyncUberEats = &syncUberEats.Bool
			}

			products[p.ProductID] = &p
			productOrder = append(productOrder, p.ProductID)
		}
	}

	// --- STEP 3: sub-products (NO available filter) ---
	subProducts := make(map[string]*models.ProductEntry)
	{
		step := "sub_products_all"
		q := `
            SELECT p.product_id, p.by_product_of, p.name, p.category, pc.categ_name, p.price, p.price_take_away, p.price_delivery, p.image_url, p.price_uber_eats, p.price_deliveroo, p.product_desc,
                   p.available_in, p.available_take_away, p.available_delivery,
                   tva_in.tva_rate as tva_rate_in, tva_delivery.tva_rate as tva_rate_delivery, tva_take_away.tva_rate as tva_rate_take_away,
				   p.bg_color, p.is_product_group, p.is_available_on_sno, p.status, p.display_order
            FROM products p
			INNER JOIN productcateg pc on pc.merchant_categ_id = p.category and pc.merchant_id = p.merchant_id
            INNER JOIN tva_categories tva_in on tva_in.tva_id = p.tva_in_id
            INNER JOIN tva_categories tva_delivery on tva_delivery.tva_id = p.tva_delivery_id
            INNER JOIN tva_categories tva_take_away on tva_take_away.tva_id = p.tva_take_away_id
            WHERE p.merchant_id = ? AND p.by_product_of IS NOT NULL
			AND p.enabled = TRUE
        `
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var p models.ProductEntry
			var by sql.NullString
			var tvaIn, tvaDel, tvaTake sql.NullFloat64
			var bg sql.NullString
			var desc, imageURL sql.NullString
			var availIn, availTake, availDel sql.NullBool
			if err := rows.Scan(&p.ProductID, &by, &p.Name, &p.CategoryID, &p.CategoryName, &p.Price, &p.PriceTakeAway, &p.PriceDelivery, &imageURL, &p.PriceUberEats, &p.PriceDeliveroo,
				&desc, &availIn, &availTake, &availDel, &tvaIn, &tvaDel, &tvaTake, &bg, &p.IsProductGroup, &p.IsAvailableOnSNO,
				&p.Status, &p.DisplayOrder); err != nil {
				return nil, err
			}
			if by.Valid {
				p.ByProductOf = &by.String
			}
			if tvaIn.Valid {
				p.TVAIn = &tvaIn.Float64
			}
			if imageURL.Valid {
				p.ImageURL = &imageURL.String
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
				p.AvailableIn = &availIn.Bool
			}
			if availTake.Valid {
				p.AvailableTakeAway = &availTake.Bool
			}
			if availDel.Valid {
				p.AvailableDelivery = &availDel.Bool
			}
			subProducts[p.ProductID] = &p
		}
	}

	// --- STEP 4: components avec calcul du coût unitaire ---
	compMap := make(map[string][]models.ComponentUsage)
	productCostMap := make(map[string]float64) // Nouveau: coût total par produit
	{
		step := "components_requires_all"
		q := `
		SELECT 
			r.product_id,
			c.component_id,
			c.name,
			rq.quantity,
			uomd.uom_desc,
			rq.unit_of_measure,
			c.purchase_price,
			c.purchase_price_quantity,
			c.unit_of_measure as purchase_uom,
			COALESCE(conv.ratio, 1) as conversion_ratio
		FROM components c
		INNER JOIN requires rq ON c.component_id = rq.component_id AND rq.enabled = true
		INNER JOIN recipes r ON r.recipe_id = rq.recipe_id
		INNER JOIN unit_of_measure_desc uomd ON uomd.lang = 'FR' AND uomd.id = rq.unit_of_measure
		LEFT JOIN unit_of_measure_convert conv ON conv.id_from = rq.unit_of_measure AND conv.id_to = c.unit_of_measure
		WHERE c.merchant_id = ?
		AND c.enabled = TRUE
		AND rq.enabled = true
	`
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var productID string
			var c models.ComponentUsage
			var uom sql.NullString
			var purchasePrice, purchasePriceQty, conversionRatio float64
			var purchaseUOM int

			if err := rows.Scan(
				&productID,
				&c.ComponentID,
				&c.Name,
				&c.Quantity,
				&uom,
				&c.UnitOfMeasureID,
				&purchasePrice,
				&purchasePriceQty,
				&purchaseUOM,
				&conversionRatio,
			); err != nil {
				return nil, err
			}

			if uom.Valid {
				c.UnitOfMeasure = uom.String
			}

			// CALCUL DU COÛT UNITAIRE
			// Formule: (quantity_used / conversion_ratio) * (purchase_price / purchase_price_quantity)
			//
			// Exemple: Olives dans ta DB
			// - Recipe utilise: 100g (rq.quantity=100, rq.unit_of_measure=2 "G")
			// - Prix d'achat: 850 pour 430g (purchase_price=850, purchase_price_quantity=430, unit_of_measure=3 "KG")
			// - Conversion G→KG: ratio=1000 (il faut 1000g pour 1kg)
			//
			// Calcul: (100 / 1000) * (850 / 430) = 0.1 * 1.977 = 0.1977 centimes

			quantityInPurchaseUnit := *c.Quantity / conversionRatio
			pricePerPurchaseUnit := purchasePrice / purchasePriceQty
			c.Cost = quantityInPurchaseUnit * pricePerPurchaseUnit

			// Vérification de validité: rejette NaN, +Inf, -Inf
			if math.IsNaN(c.Cost) || math.IsInf(c.Cost, 0) {
				// Invalide: mettre à 0 et ne pas accumuler
				c.Cost = 0
			} else {
				// Arrondir à 2 décimales (centimes)
				c.Cost = math.Round(c.Cost*100) / 100
				// Valide: accumuler le coût total du produit
				productCostMap[productID] += c.Cost
			}

			compMap[productID] = append(compMap[productID], c)
		}
	}

	// Plus tard, quand tu attaches les composants aux produits:
	for i := range products {
		pid := products[i].ProductID

		// Attache les composants
		if comps, ok := compMap[pid]; ok {
			products[i].Components = comps
		}

		// Calcule le foodcost
		if totalCost, ok := productCostMap[pid]; ok {
			// Vérification de validité: rejette NaN, +Inf, -Inf
			if math.IsNaN(totalCost) || math.IsInf(totalCost, 0) {
				// Invalide: mettre CostPrice à nil
				products[i].CostPrice = nil
			} else {
				// Arrondir totalCost à 2 décimales (centimes)
				totalCostRounded := math.Round(totalCost*100) / 100
				products[i].CostPrice = &totalCostRounded

				// Calcul du foodcost en pourcentage
				// Formule: (cost_price / price) * 100
				// Les deux sont en centimes, donc ça s'annule
				// Exemple: 508 centimes / 1200 centimes = 0.4233 → 42.33%
				if products[i].Price > 0 {
					foodCostPercent := (totalCostRounded / float64(products[i].Price)) * 100
					// Arrondir foodcost_percent à 2 décimales
					foodCostPercentRounded := math.Round(foodCostPercent*100) / 100
					products[i].FoodCostPercent = &foodCostPercentRounded
					// Arrondir margin_percent à 2 décimales
					marginPercentRounded := math.Round((100-foodCostPercentRounded)*100) / 100
					products[i].MarginPercent = &marginPercentRounded
				}
			}
		}
	}

	// --- STEP 5: configurable attributes + options (NO availability filter) ---
	optMap := make(map[string][]models.ConfigurableOption)
	{
		step := "configurable_options_all"
		q := `
            SELECT DISTINCT ca.id as configurable_attribute_id, cao.id, cao.title, cao.extra_price, cao.max_quantity, cao.image_url
            FROM products p
            INNER JOIN product_configurable_attribute pca on pca.product_id = ` + menuCastChar("p.product_id") + `
            INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
            INNER JOIN configurable_attribute_options cao on cao.configurable_attribute_id = ca.id
            WHERE p.merchant_id = ? AND ca.enabled = TRUE AND cao.enabled = 1 AND pca.enabled = TRUE
			AND p.enabled = TRUE
        `
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var cfgID string
			var o models.ConfigurableOption
			if err := rows.Scan(&cfgID, &o.ID, &o.Title, &o.ExtraPrice, &o.MaxQuantity, &o.ImageURL); err != nil {
				return nil, err
			}
			optMap[cfgID] = append(optMap[cfgID], o)
		}
	}

	attrMap := make(map[string][]models.ConfigurableAttribute)
	{
		step := "configurable_attributes_all"
		q := `
            SELECT ca.id, pca.product_id, ca.title, ca.max_options, ca.attribute_type, ca.min_options
            FROM products p
            INNER JOIN product_configurable_attribute pca on pca.product_id = ` + menuCastChar("p.product_id") + `
            INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
            WHERE p.merchant_id = ? AND ca.enabled = TRUE AND pca.enabled = TRUE
			AND p.enabled = TRUE
            ORDER BY pca.num_order ASC
        `
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var a models.ConfigurableAttribute
			if err := rows.Scan(&a.ID, &a.ProductID, &a.Title, &a.MaxOptions, &a.AttributeType, &a.MinOptions); err != nil {
				return nil, err
			}
			a.Options = optMap[a.ID]
			attrMap[a.ProductID] = append(attrMap[a.ProductID], a)
		}
	}

	// --- STEP 6: allergens per product (NO product availability filter) ---
	allergenMap := make(map[string][]models.AllergenEntry)
	{
		q := `
			SELECT pa.product_id, a.allergen_id, a.name, a.code, COALESCE(a.icon, '')
			FROM product_allergens pa
			INNER JOIN allergens a ON a.allergen_id = pa.allergen_id
			WHERE pa.product_id IN (
				SELECT ` + menuCastChar("product_id") + ` FROM products WHERE merchant_id = ? AND enabled = TRUE
			)
		`
		rows, err := runQuery("allergens_per_product_all", q, merchantID)
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

	// --- STEP 7: tags per product (NO product availability filter) ---
	tagMap := make(map[string][]models.TagEntry)
	{
		q := `
			SELECT pt.product_id, t.tag_id, t.name, t.color, COALESCE(t.display_order, 0) as display_order
			FROM product_tags pt
			INNER JOIN tags t ON t.tag_id = pt.tag_id
			WHERE t.merchant_id = ? AND pt.product_id IN (
				SELECT ` + menuCastChar("product_id") + ` FROM products WHERE merchant_id = ? AND enabled = TRUE
			)
			ORDER BY t.display_order ASC
		`
		rows, err := runQuery("tags_per_product_all", q, merchantID, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var productID string
			var t models.TagEntry
			if err := rows.Scan(&productID, &t.ID, &t.Name, &t.Color, &t.DisplayOrder); err != nil {
				return nil, err
			}
			tagMap[productID] = append(tagMap[productID], t)
		}
	}

	// --- BUILD: attach sub-products to parents & attach components & configuration (same as GetMenu) ---
	// enrich root products and sub-products before attachment so nested entries include same details
	for _, bucket := range []map[string]*models.ProductEntry{products, subProducts} {
		for _, p := range bucket {
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
	}

	// attach subproducts
	for _, sp := range subProducts {
		if sp.ByProductOf != nil {
			if parent, ok := products[*sp.ByProductOf]; ok && parent != nil {
				parent.SubProducts = append(parent.SubProducts, *sp)
			}
		}
	}

	// --- build categories -> products ---
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
		var categoryImageURL *string
		if c.Image.Valid {
			categoryImageURL = &c.Image.String
		}
		productTypes = append(productTypes, models.ProductCategory{
			CategoryName: c.Name,
			CategoryID:   c.ID,
			Order:        c.Order,
			BgColor:      bg,
			ImageURL:     categoryImageURL,
			Available:    c.Available,
			Products:     actual,
		})
	}

	return productTypes, nil
}

func (r *MenuRepository) GetAllComponents(ctx context.Context, merchantID string) ([]models.ComponentCategory, error) {
	db := dbx.GetDB(ctx, r.database)

	runQuery := func(step string, query string, args ...interface{}) (*sql.Rows, error) {
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("%s query error: %w", step, err)
		}
		return rows, nil
	}

	// --- STEP 1: component categories (NO available filter) ---
	type compCatTmp struct {
		ID    string
		Name  string
		Order int
	}
	var compCats []compCatTmp
	{
		step := "component_categories_all"
		q := `SELECT merchant_categ_id, name, categ_order FROM component_category WHERE merchant_id = ? and enabled = TRUE ORDER BY categ_order ASC`
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var c compCatTmp
			if err := rows.Scan(&c.ID, &c.Name, &c.Order); err != nil {
				return nil, err
			}
			compCats = append(compCats, c)
		}
	}

	// --- STEP 2: all components (NO available filter) ---
	type compBasicTmp struct {
		ID                      string
		Name                    string
		CatID                   *string
		Status                  string
		Price                   int
		UnitOfMeasureID         int
		UnitOfMeasure           sql.NullString
		UnitShortName           sql.NullString
		PurchasePrice           sql.NullInt64
		PurchasePriceQty        sql.NullFloat64
		PurchaseUnitOfMeasureID sql.NullInt64
		PurchaseUnitOfMeasure   sql.NullString
		ConservationDays        sql.NullInt64
		ConservationType        sql.NullString
		StorageTempMin          sql.NullFloat64
		StorageTempMax          sql.NullFloat64
	}
	var allComponents []compBasicTmp
	{
		step := "all_components"
		q := `
			SELECT
				c.component_id,
				c.name,
				c.category_id,
				c.status,
				c.component_price,
				c.unit_of_measure,
				COALESCE(uomd.uom_desc, '') as uom_desc,
				COALESCE(uomd.uom_short_desc, '') as uom_short_desc,
				c.purchase_price,
				c.purchase_price_quantity,
				c.purchase_unit_id,
				COALESCE(puomd.uom_desc, '') as purchase_uom_desc,
				c.conservation_days,
				c.conservation_type,
				c.storage_temp_min,
				c.storage_temp_max
			FROM components c
			LEFT JOIN unit_of_measure_desc uomd ON uomd.lang = 'FR' AND uomd.id = c.unit_of_measure
			LEFT JOIN unit_of_measure_desc puomd ON puomd.lang = 'FR' AND ` + menuCastChar("puomd.id") + ` = c.purchase_unit_id
			WHERE c.merchant_id = ? and c.enabled = TRUE
		`
		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var cb compBasicTmp
			if err := rows.Scan(&cb.ID, &cb.Name, &cb.CatID, &cb.Status, &cb.Price, &cb.UnitOfMeasureID, &cb.UnitOfMeasure, &cb.UnitShortName, &cb.PurchasePrice, &cb.PurchasePriceQty, &cb.PurchaseUnitOfMeasureID, &cb.PurchaseUnitOfMeasure, &cb.ConservationDays, &cb.ConservationType, &cb.StorageTempMin, &cb.StorageTempMax); err != nil {
				return nil, err
			}
			allComponents = append(allComponents, cb)
		}
	}

	// --- Build component types (same logic as GetMenu) ---
	compTypes := []models.ComponentCategory{}
	for _, cc := range compCats {
		actual := []models.ComponentBasic{}
		for _, cb := range allComponents {
			if cb.CatID != nil && *cb.CatID == cc.ID {
				uomID := fmt.Sprintf("%d", cb.UnitOfMeasureID)
				uomName := ""
				if cb.UnitOfMeasure.Valid {
					uomName = cb.UnitOfMeasure.String
				}

				// Convertir les valeurs d'achat nullables en pointeurs
				var purchasePrice *int
				if cb.PurchasePrice.Valid {
					pp := int(cb.PurchasePrice.Int64)
					purchasePrice = &pp
				}

				var purchasePriceQty *float64
				if cb.PurchasePriceQty.Valid {
					ppq := cb.PurchasePriceQty.Float64
					purchasePriceQty = &ppq
				}

				// Calculer purchase_price_per_unit = purchase_price / purchase_price_quantity
				var purchasePricePerUnit *float64
				if purchasePrice != nil && purchasePriceQty != nil && *purchasePriceQty > 0 {
					ppu := float64(*purchasePrice) / *purchasePriceQty
					purchasePricePerUnit = &ppu
				}

				purchaseUomID := ""
				if cb.PurchaseUnitOfMeasureID.Valid {
					purchaseUomID = fmt.Sprintf("%d", cb.PurchaseUnitOfMeasureID.Int64)
				}
				purchaseUomName := ""
				if cb.PurchaseUnitOfMeasure.Valid {
					purchaseUomName = cb.PurchaseUnitOfMeasure.String
				}

				var conservationDays *int
				if cb.ConservationDays.Valid {
					cd := int(cb.ConservationDays.Int64)
					conservationDays = &cd
				}

				conservationType := "froid"
				if cb.ConservationType.Valid && cb.ConservationType.String != "" {
					conservationType = cb.ConservationType.String
				}

				var storageTempMin *float64
				if cb.StorageTempMin.Valid {
					v := cb.StorageTempMin.Float64
					storageTempMin = &v
				}

				var storageTempMax *float64
				if cb.StorageTempMax.Valid {
					v := cb.StorageTempMax.Float64
					storageTempMax = &v
				}

				actual = append(actual, models.ComponentBasic{
					ComponentID:             cb.ID,
					Name:                    cb.Name,
					Category:                cb.CatID,
					Price:                   cb.Price,
					Status:                  cb.Status,
					UnitOfMeasureID:         uomID,
					UnitOfMeasure:           uomName,
					PurchasePrice:           purchasePrice,
					PurchasePriceQty:        purchasePriceQty,
					PurchasePricePerUnit:    purchasePricePerUnit,
					PurchaseUnitOfMeasureID: purchaseUomID,
					PurchaseUnitOfMeasure:   purchaseUomName,
					UnitOfMeasureShortName:  cb.UnitShortName.String,
					ConservationDays:        conservationDays,
					ConservationType:        conservationType,
					StorageTempMin:          storageTempMin,
					StorageTempMax:          storageTempMax,
				})
			}
		}
		compTypes = append(compTypes, models.ComponentCategory{
			CategoryName: cc.Name,
			CategoryID:   cc.ID,
			Order:        cc.Order,
			Components:   actual,
		})
	}

	return compTypes, nil
}

// GetComponent retrieves a single component by ID
func (r *MenuRepository) GetComponent(ctx context.Context, merchantID, componentID string) (*models.ComponentBasic, error) {
	db := dbx.GetDB(ctx, r.database)

	q := `
		SELECT
			c.component_id,
			c.name,
			c.category_id,
			c.status,
			c.component_price,
			c.unit_of_measure,
			COALESCE(uomd.uom_desc, '') as uom_desc,
			COALESCE(uomd.uom_short_desc, '') as uom_short_desc,
			c.purchase_price,
			c.purchase_price_quantity,
			c.purchase_unit_id,
			COALESCE(puomd.uom_desc, '') as purchase_uom_desc,
			c.conservation_days,
			c.conservation_type,
			c.storage_temp_min,
			c.storage_temp_max
		FROM components c
		LEFT JOIN unit_of_measure_desc uomd ON uomd.lang = 'FR' AND uomd.id = c.unit_of_measure
		LEFT JOIN unit_of_measure_desc puomd ON puomd.lang = 'FR' AND ` + menuCastChar("puomd.id") + ` = c.purchase_unit_id
		WHERE c.component_id = ? AND c.merchant_id = ? AND c.enabled = TRUE
	`

	var (
		id                      string
		name                    string
		catID                   *string
		status                  string
		price                   int
		unitOfMeasureID         int
		unitOfMeasure           sql.NullString
		unitShortName           sql.NullString
		purchasePrice           sql.NullInt64
		purchasePriceQty        sql.NullFloat64
		purchaseUnitOfMeasureID sql.NullInt64
		purchaseUnitOfMeasure   sql.NullString
		conservationDays        sql.NullInt64
		conservationType        sql.NullString
		storageTempMin          sql.NullFloat64
		storageTempMax          sql.NullFloat64
	)

	err := db.QueryRowContext(ctx, q, componentID, merchantID).Scan(
		&id, &name, &catID, &status, &price, &unitOfMeasureID, &unitOfMeasure, &unitShortName, &purchasePrice, &purchasePriceQty, &purchaseUnitOfMeasureID, &purchaseUnitOfMeasure,
		&conservationDays, &conservationType, &storageTempMin, &storageTempMax,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("component_not_found")
		}
		return nil, fmt.Errorf("get component query error: %w", err)
	}

	uomID := fmt.Sprintf("%d", unitOfMeasureID)
	uomName := ""
	if unitOfMeasure.Valid {
		uomName = unitOfMeasure.String
	}

	var pp *int
	if purchasePrice.Valid {
		ppVal := int(purchasePrice.Int64)
		pp = &ppVal
	}

	var ppq *float64
	if purchasePriceQty.Valid {
		ppqVal := purchasePriceQty.Float64
		ppq = &ppqVal
	}

	purchaseUomID := ""
	if purchaseUnitOfMeasureID.Valid {
		purchaseUomID = fmt.Sprintf("%d", purchaseUnitOfMeasureID.Int64)
	}
	purchaseUomName := ""
	if purchaseUnitOfMeasure.Valid {
		purchaseUomName = purchaseUnitOfMeasure.String
	}

	var cd *int
	if conservationDays.Valid {
		cdVal := int(conservationDays.Int64)
		cd = &cdVal
	}

	ct := "froid"
	if conservationType.Valid && conservationType.String != "" {
		ct = conservationType.String
	}

	var stMin *float64
	if storageTempMin.Valid {
		stMinVal := storageTempMin.Float64
		stMin = &stMinVal
	}

	var stMax *float64
	if storageTempMax.Valid {
		stMaxVal := storageTempMax.Float64
		stMax = &stMaxVal
	}

	return &models.ComponentBasic{
		ComponentID:             id,
		Name:                    name,
		Category:                catID,
		Price:                   price,
		Status:                  status,
		UnitOfMeasureID:         uomID,
		UnitOfMeasure:           uomName,
		UnitOfMeasureShortName:  unitShortName.String,
		PurchasePrice:           pp,
		PurchasePriceQty:        ppq,
		PurchaseUnitOfMeasureID: purchaseUomID,
		PurchaseUnitOfMeasure:   purchaseUomName,
		ConservationDays:        cd,
		ConservationType:        ct,
		StorageTempMin:          stMin,
		StorageTempMax:          stMax,
	}, nil
}

// CreateProduct est l'enveloppe unitaire du chemin POST /menu/products :
// validations 1 à 4, puis contrôle d'unicité du nom avec sa confirmation Redis
// (validation 5, isolée ici parce qu'elle porte un effet de bord), puis
// insertion transactionnelle et marquage du menu comme modifié.
//
// Statut du produit créé : CreateProductPayload.Status étant un *string, un
// champ absent laisse la colonne hors de l'INSERT et c'est le défaut SQL qui
// s'applique — products.status DEFAULT '1', identique en MySQL et en Postgres.
// Fourni, il est écrit brut, sans validation. Les deux fiches du back-office
// divergent d'ailleurs aujourd'hui : ProductCreateSheet n'envoie pas le champ
// (donc '1') tandis que SimpleProductSheet envoie 'available'. Les deux valeurs
// coexistent donc en base pour des produits équivalents — d'où le filtre
// p.status IN ('available', '1') de ListAvailableProductsForUpsell.
func (r *MenuRepository) CreateProduct(ctx context.Context, p *CreateProductPayload) (string, error) {
	if err := r.validateProductForCreate(ctx, p); err != nil {
		return "0", err
	}

	db := dbx.GetDB(ctx, r.database)

	// --- VALIDATION 5: Vérifier qu'aucun produit actif ne porte déjà ce nom ---
	var productNameExists int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM products WHERE merchant_id = ? AND LOWER(name) = LOWER(?) AND enabled = TRUE`,
		p.MerchantID, strings.TrimSpace(p.Name),
	).Scan(&productNameExists)
	if err != nil {
		return "0", fmt.Errorf("failed to check product name uniqueness: %w", err)
	}
	if productNameExists > 0 {
		if r.redis == nil {
			return "0", models.ErrProductNameAlreadyExists
		}
		// Redis actif : le 1er appel pose une clé de confirmation et bloque ;
		// un 2e appel identique (même merchant + même nom) la trouve déjà
		// posée et la création est acceptée malgré le doublon.
		confirmKey := helpers.GetMenuProductNameConfirmKey(p.MerchantID, p.Name)
		if r.redis.SetNX(ctx, confirmKey, "1", models.MenuNameConfirmTTL) {
			return "0", models.ErrProductNameAlreadyExistsWithRetry
		}
		r.redis.Delete(ctx, confirmKey)
	}

	// Le produit et ses associations (composition, options, tags, allergènes,
	// intégrations) sont écrits dans une seule transaction : un échec sur une
	// association annule la création au lieu de laisser en base un produit
	// incomplet que l'appelant croit ne pas avoir créé.
	var productID string
	err = dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		id, insertErr := r.insertProductTx(txCtx, p)
		if insertErr != nil {
			return insertErr
		}
		productID = id

		_ = r.setMenuUpdated(txCtx, p.MerchantID)

		return nil
	})
	if err != nil {
		return "0", err
	}

	return productID, nil
}

// validateProductForCreate exécute les validations 1 à 4 de la création d'un
// produit : champs TVA obligatoires et numériques, catégorie existante et
// activée, puis existence des trois taux de TVA. Ces contrôles sont en lecture
// seule et rejouables tels quels.
//
// La validation 5 (unicité du nom) en est volontairement absente : elle porte
// un effet de bord — la clé de confirmation Redis qui autorise un doublon au
// second appel — et reste donc dans l'enveloppe unitaire CreateProduct.
func (r *MenuRepository) validateProductForCreate(ctx context.Context, p *CreateProductPayload) error {
	db := dbx.GetDB(ctx, r.database)

	// --- VALIDATION 1: Vérifier les champs obligatoires de TVA ---
	if p.TvaInID == "" {
		return fmt.Errorf("tva_in_id is required")
	}
	if p.TvaDeliveryID == "" {
		return fmt.Errorf("tva_delivery_id is required")
	}
	if p.TvaTakeAwayID == "" {
		return fmt.Errorf("tva_take_away_id is required")
	}
	// tva_id est un integer : un ID non numérique valait 0 en MySQL non-strict
	// (aucune correspondance) — même refus côté Go, sans erreur de type Postgres.
	if !menuNumericID(p.TvaInID) {
		return fmt.Errorf("tva_in_id '%s' does not exist or is disabled", p.TvaInID)
	}
	if !menuNumericID(p.TvaDeliveryID) {
		return fmt.Errorf("tva_delivery_id '%s' does not exist or is disabled", p.TvaDeliveryID)
	}
	if !menuNumericID(p.TvaTakeAwayID) {
		return fmt.Errorf("tva_take_away_id '%s' does not exist or is disabled", p.TvaTakeAwayID)
	}

	// --- VALIDATION 2: Vérifier que la catégorie existe et est activée ---
	var categoryExists int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM productcateg WHERE merchant_categ_id = ? AND merchant_id = ? AND enabled = TRUE`,
		p.CategoryID, p.MerchantID,
	).Scan(&categoryExists)
	if err != nil {
		return fmt.Errorf("failed to check category existence: %w", err)
	}
	if categoryExists == 0 {
		return fmt.Errorf("category does not exist or is disabled")
	}

	// --- VALIDATION 3: Vérifier que tous les taux de TVA existent et sont activés ---
	var tvaCount int
	err = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tva_categories 
		 WHERE enabled = TRUE AND tva_id IN (?, ?, ?)`,
		p.TvaInID, p.TvaDeliveryID, p.TvaTakeAwayID,
	).Scan(&tvaCount)
	if err != nil {
		return fmt.Errorf("failed to check tva rates existence: %w", err)
	}
	if tvaCount != 3 {
		return fmt.Errorf("one or more tva rates do not exist or are disabled: provided %d but found %d", 3, tvaCount)
	}

	// --- VALIDATION 4: Vérifier que les taux TVA existent tous dans les résultats ---
	var tvaInExists, tvaDeliveryExists, tvaTakeAwayExists int
	err = db.QueryRowContext(ctx,
		`SELECT 
			(SELECT COUNT(*) FROM tva_categories WHERE enabled = TRUE AND tva_id = ?) as tva_in_count,
			(SELECT COUNT(*) FROM tva_categories WHERE enabled = TRUE AND tva_id = ?) as tva_delivery_count,
			(SELECT COUNT(*) FROM tva_categories WHERE enabled = TRUE AND tva_id = ?) as tva_take_away_count`,
		p.TvaInID, p.TvaDeliveryID, p.TvaTakeAwayID,
	).Scan(&tvaInExists, &tvaDeliveryExists, &tvaTakeAwayExists)
	if err != nil {
		return fmt.Errorf("failed to validate individual tva rates: %w", err)
	}
	if tvaInExists == 0 {
		return fmt.Errorf("tva_in_id '%s' does not exist or is disabled", p.TvaInID)
	}
	if tvaDeliveryExists == 0 {
		return fmt.Errorf("tva_delivery_id '%s' does not exist or is disabled", p.TvaDeliveryID)
	}
	if tvaTakeAwayExists == 0 {
		return fmt.Errorf("tva_take_away_id '%s' does not exist or is disabled", p.TvaTakeAwayID)
	}

	return nil
}

// insertProductTx insère le produit puis ses associations (options, composition,
// tags, allergènes) et retourne l'ID généré. Volontairement dépourvu de tout
// contrôle d'unicité, de setMenuUpdated, d'invalidation de cache et de
// rattachement à une catégorie marketing : ces effets restent à la charge de
// l'appelant.
//
// Il n'ouvre pas de transaction propre et s'exécute dans celle portée par ctx
// (dbx.GetDB). RunInTx étant réentrant, un appelant en lot peut envelopper
// plusieurs appels dans une transaction unique.
func (r *MenuRepository) insertProductTx(ctx context.Context, p *CreateProductPayload) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	// Calculer le prix le plus haut entre price, price_delivery, et price_takeaway.
	// Les prix arrivent en float64 (JSON) sur des colonnes integer : MySQL
	// non-strict arrondissait silencieusement, pgx refuse d'encoder un float
	// sur un integer -> arrondi Go (même valeur stockée, cf. cash_fund Tier 3).
	price := int(math.Round(p.Price))
	priceTakeAway := int(math.Round(p.PriceTakeAway))
	priceDelivery := int(math.Round(p.PriceDelivery))
	maxPrice := price
	if priceDelivery > maxPrice {
		maxPrice = priceDelivery
	}
	if priceTakeAway > maxPrice {
		maxPrice = priceTakeAway
	}

	// Les prix plateformes valent le prix le plus élevé, sauf si la fiche impose
	// un prix dédié.
	uberEatsPrice := maxPrice
	deliverooPrice := maxPrice
	if p.Integrations != nil {
		if p.Integrations.UberEats.PriceOverride != nil {
			uberEatsPrice = *p.Integrations.UberEats.PriceOverride
		}
		if p.Integrations.Deliveroo.PriceOverride != nil {
			deliverooPrice = *p.Integrations.Deliveroo.PriceOverride
		}
	}

	columns := []string{
		"merchant_id",
		"name",
		"product_desc",
		"price",
		"price_take_away",
		"price_delivery",
		"price_uber_eats",
		"price_deliveroo",
		"tva_in_id",
		"tva_delivery_id",
		"tva_take_away_id",
		"category",
		"is_product_group",
	}
	args := []interface{}{
		p.MerchantID,
		p.Name,
		p.ProductDesc,
		price,
		priceTakeAway,
		priceDelivery,
		uberEatsPrice,
		deliverooPrice,
		p.TvaInID,
		p.TvaDeliveryID,
		p.TvaTakeAwayID,
		p.CategoryID,
		p.IsProductGroup,
	}

	// Colonnes facultatives : seules celles fournies sont insérées, pour que
	// les autres gardent le défaut de la colonne — un pointeur nil passé tel
	// quel insérerait NULL et écraserait ce défaut.
	addOptional := func(column string, value interface{}) {
		columns = append(columns, column)
		args = append(args, value)
	}
	if p.BgColor != nil {
		addOptional("bg_color", *p.BgColor)
	}
	if p.ProductionColor != nil {
		addOptional("production_color", *p.ProductionColor)
	}
	if p.Status != nil {
		addOptional("status", *p.Status)
	}
	if p.IsAvailableOnSno != nil {
		addOptional("is_available_on_sno", *p.IsAvailableOnSno)
	}
	if p.AvailableIn != nil {
		addOptional("available_in", *p.AvailableIn)
	}
	if p.AvailableTakeAway != nil {
		addOptional("available_take_away", *p.AvailableTakeAway)
	}
	if p.AvailableDelivery != nil {
		addOptional("available_delivery", *p.AvailableDelivery)
	}
	// Écrit explicitement plutôt que délégué à SyncProductIntegrations : ce
	// dernier ne touche à rien quand les deux canaux sont désactivés, ce qui
	// laissait les colonnes à leur défaut TRUE — un produit créé avec Uber Eats
	// désactivé ressortait actif.
	if p.Integrations != nil {
		addOptional("sync_uber_eats", p.Integrations.UberEats.Enabled)
		addOptional("sync_deliveroo", p.Integrations.Deliveroo.Enabled)
	}

	query := `INSERT INTO products (` + strings.Join(columns, ", ") + `) VALUES (` +
		strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ") + `)`

	id, insertErr := db.InsertReturningID(ctx, query, "product_id", args...)
	if insertErr != nil {
		return "", insertErr
	}
	productID := strconv.FormatInt(id, 10)

	if len(p.Configuration) > 0 {
		if err := r.SyncProductAttributes(ctx, p.MerchantID, productID, p.Configuration); err != nil {
			return "", fmt.Errorf("failed to sync product attributes: %w", err)
		}
	}

	if len(p.Components) > 0 {
		if err := r.SyncProductComponents(ctx, p.MerchantID, productID, p.Components); err != nil {
			return "", fmt.Errorf("failed to sync product components: %w", err)
		}
	}

	if len(p.Tags) > 0 {
		if err := r.SyncProductTags(ctx, p.MerchantID, productID, p.Tags); err != nil {
			return "", fmt.Errorf("failed to sync product tags: %w", err)
		}
	}

	if len(p.Allergens) > 0 {
		if err := r.SyncProductAllergens(ctx, p.MerchantID, productID, p.Allergens); err != nil {
			return "", fmt.Errorf("failed to sync product allergens: %w", err)
		}
	}

	// Les intégrations (sync_* et prix plateformes) sont déjà portées par
	// l'INSERT ci-dessus — pas de synchronisation séparée ici.

	return productID, nil
}

func (r *MenuRepository) CreateExternalProductTx(ctx context.Context, merchantID, name, description string, price int) (int64, error) {
	db := dbx.GetDB(ctx, r.database)

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
		VALUES (?, ?, ?, 'UBER_EATS_TEMP', ?, FALSE, 5, 9, 3)
	`

	newID, err := db.InsertReturningID(ctx, query, "product_id",
		merchantID,
		name,
		description,
		price,
	)
	if err != nil {
		return 0, err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return newID, nil
}

func (r *MenuRepository) GetProduct(ctx context.Context, merchantID, productID string) (*models.ProductEntry, error) {
	db := dbx.GetDB(ctx, r.database)

	// Helper function to run queries with logging
	runQuery := func(step string, query string, args ...interface{}) (*sql.Rows, error) {
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("%s query error: %w", step, err)
		}
		return rows, nil
	}

	// --- STEP 1: Get product ---
	query := `
		SELECT
			product_id,
			merchant_id,
			name,
			product_desc,
			price,
			price_take_away,
			price_delivery,
			price_uber_eats,
			price_deliveroo,
			category,
			is_product_group,
			available_in,
			available_take_away,
			available_delivery,
			tva_in.tva_rate as tva_rate_in,
			tva_delivery.tva_rate as tva_rate_delivery,
			tva_take_away.tva_rate as tva_rate_take_away,
			bg_color,
			production_color,
			status,
			sync_uber_eats,
			sync_deliveroo,
			is_available_on_sno,
			available,
			image_url
		FROM products p
		INNER JOIN tva_categories tva_in ON tva_in.tva_id = p.tva_in_id
		INNER JOIN tva_categories tva_delivery ON tva_delivery.tva_id = p.tva_delivery_id
		INNER JOIN tva_categories tva_take_away ON tva_take_away.tva_id = p.tva_take_away_id
		WHERE p.merchant_id = ? AND p.product_id = ?
		LIMIT 1
	`

	var p models.ProductEntry
	var syncUberEats bool
	var syncDeliveroo bool
	var priceUberEats sql.NullInt64
	var priceDeliveroo sql.NullInt64
	var tvaIn sql.NullFloat64
	var tvaDelivery sql.NullFloat64
	var tvaTakeAway sql.NullFloat64

	err := db.QueryRowContext(ctx, query, merchantID, productID).Scan(
		&p.ProductID,
		&p.MerchantID,
		&p.Name,
		&p.Description,
		&p.Price,
		&p.PriceTakeAway,
		&p.PriceDelivery,
		&priceUberEats,
		&priceDeliveroo,
		&p.Category,
		&p.IsProductGroup,
		&p.AvailableIn,
		&p.AvailableTakeAway,
		&p.AvailableDelivery,
		&tvaIn,
		&tvaDelivery,
		&tvaTakeAway,
		&p.BgColor,
		&p.ProductionColor,
		&p.Status,
		&syncUberEats,
		&syncDeliveroo,
		&p.IsAvailableOnSNO,
		&p.Available,
		&p.ImageURL,
	)

	if err != nil {
		return nil, err
	}

	if tvaIn.Valid {
		p.TVAIn = &tvaIn.Float64
	}
	if tvaDelivery.Valid {
		p.TVADelivery = &tvaDelivery.Float64
	}
	if tvaTakeAway.Valid {
		p.TVATakeAway = &tvaTakeAway.Float64
	}

	// Build integrations object
	p.Integrations = models.ProductIntegrations{
		UberEats: models.ProductIntegrationItem{
			Enabled: syncUberEats,
			PriceOverride: func() *int {
				if priceUberEats.Valid {
					val := int(priceUberEats.Int64)
					return &val
				}
				return nil
			}(),
		},
		Deliveroo: models.ProductIntegrationItem{
			Enabled: syncDeliveroo,
			PriceOverride: func() *int {
				if priceDeliveroo.Valid {
					val := int(priceDeliveroo.Int64)
					return &val
				}
				return nil
			}(),
		},
	}

	// --- STEP 2: Load components (requires) ---
	compSlice := []models.ComponentUsage{}
	{
		step := "components_for_product"
		q := `
			SELECT c.component_id, c.name, rq.quantity, uomd.uom_desc, rq.unit_of_measure
			FROM components c
			INNER JOIN requires rq ON c.component_id = rq.component_id AND rq.enabled = true
			INNER JOIN recipes r ON r.recipe_id = rq.recipe_id AND r.product_id = ?
			INNER JOIN unit_of_measure_desc uomd ON uomd.lang = 'FR' AND uomd.id = rq.unit_of_measure
			WHERE c.merchant_id = ? AND c.available = TRUE
		`
		rows, err := runQuery(step, q, productID, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var c models.ComponentUsage
			var uom sql.NullString
			if err := rows.Scan(&c.ComponentID, &c.Name, &c.Quantity, &uom, &c.UnitOfMeasureID); err != nil {
				return nil, err
			}
			if uom.Valid {
				c.UnitOfMeasure = uom.String
			}
			compSlice = append(compSlice, c)
		}
	}
	p.Components = compSlice

	// --- STEP 3: Load configurable options ---
	optMap := make(map[string][]models.ConfigurableOption)
	{
		step := "configurable_options_for_product"
		q := `
			SELECT DISTINCT ca.id, cao.id, cao.title, cao.extra_price, cao.max_quantity, cao.image_url
			FROM product_configurable_attribute pca
			INNER JOIN configurable_attributes ca ON ca.id = pca.configurable_attribute_id
			INNER JOIN configurable_attribute_options cao ON cao.configurable_attribute_id = ca.id
			WHERE pca.product_id = ? AND ca.enabled = TRUE AND cao.enabled = 1 AND pca.enabled = TRUE
		`
		rows, err := runQuery(step, q, productID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var cfgID string
			var o models.ConfigurableOption
			if err := rows.Scan(&cfgID, &o.ID, &o.Title, &o.ExtraPrice, &o.MaxQuantity, &o.ImageURL); err != nil {
				return nil, err
			}
			optMap[cfgID] = append(optMap[cfgID], o)
		}
	}

	// --- STEP 4: Load configurable attributes ---
	attrSlice := []models.ConfigurableAttribute{}
	{
		step := "configurable_attributes_for_product"
		q := `
			SELECT ca.id, ca.title, ca.max_options, ca.attribute_type, ca.min_options
			FROM product_configurable_attribute pca
			INNER JOIN configurable_attributes ca ON ca.id = pca.configurable_attribute_id
			WHERE pca.product_id = ? AND ca.enabled = TRUE AND pca.enabled = TRUE
			ORDER BY pca.num_order ASC
		`
		rows, err := runQuery(step, q, productID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var a models.ConfigurableAttribute
			if err := rows.Scan(&a.ID, &a.Title, &a.MaxOptions, &a.AttributeType, &a.MinOptions); err != nil {
				return nil, err
			}
			a.ProductID = productID
			a.Options = optMap[a.ID]
			attrSlice = append(attrSlice, a)
		}
	}
	p.Configuration = models.ConfigurableResponse{Attributes: attrSlice}

	// --- STEP 5: Load allergens ---
	allergenSlice := []models.AllergenEntry{}
	{
		q := `
			SELECT a.allergen_id, a.name, a.code, COALESCE(a.icon, '')
			FROM product_allergens pa
			INNER JOIN allergens a ON a.allergen_id = pa.allergen_id
			WHERE pa.product_id = ?
		`
		rows, err := runQuery("allergens_for_product", q, productID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var a models.AllergenEntry
			if err := rows.Scan(&a.ID, &a.Name, &a.Code, &a.Icon); err != nil {
				return nil, err
			}
			allergenSlice = append(allergenSlice, a)
		}
	}
	p.Allergens = allergenSlice

	// --- STEP 6: Load tags ---
	tagSlice := []models.TagEntry{}
	{
		q := `
			SELECT t.tag_id, t.name, COALESCE(t.display_order, 0) as display_order
			FROM product_tags pt
			INNER JOIN tags t ON t.tag_id = pt.tag_id
			WHERE t.merchant_id = ? AND pt.product_id = ?
			ORDER BY t.display_order ASC
		`
		rows, err := runQuery("tags_for_product", q, merchantID, productID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var t models.TagEntry
			if err := rows.Scan(&t.ID, &t.Name, &t.DisplayOrder); err != nil {
				return nil, err
			}
			tagSlice = append(tagSlice, t)
		}
	}
	p.Tags = tagSlice

	return &p, nil
}

func (r *MenuRepository) SetComponentStatus(ctx context.Context, merchantID, cid, status string) (int64, error) {
	db := dbx.GetDB(ctx, r.database)

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

func (r *MenuRepository) SetProductStatus(ctx context.Context, merchantID, pid, status string) (int64, error) {
	db := dbx.GetDB(ctx, r.database)

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

func (r *MenuRepository) SetProductCategoryAvailability(ctx context.Context, merchantID, categoryID, status string) (int64, error) {
	db := dbx.GetDB(ctx, r.database)

	boolStatus := status == "1" || status == "true" || status == "TRUE" || status == "True" // Normalize to "1" or "0"

	res, err := db.ExecContext(ctx,
		`UPDATE productcateg 
		 SET available = ?
		 WHERE merchant_categ_id = ? AND merchant_id = ?`,
		boolStatus, categoryID, merchantID,
	)
	if err != nil {
		return 0, err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return res.RowsAffected()
}

func (r *MenuRepository) SetProductAvailability(ctx context.Context, merchantID, productID, status string) (int64, error) {
	db := dbx.GetDB(ctx, r.database)

	boolStatus := status == "1" || status == "true" || status == "TRUE" || status == "True" // Normalize to "1" or "0"

	res, err := db.ExecContext(ctx,
		`UPDATE products 
		 SET available = ?
		 WHERE product_id = ? AND merchant_id = ?`,
		boolStatus, productID, merchantID,
	)
	if err != nil {
		return 0, err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return res.RowsAffected()
}

func (r *MenuRepository) UpdateProductCategory(ctx context.Context, merchantID, categoryID, name string) error {
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx,
		`UPDATE productcateg 
			 SET categ_name = ?
			 WHERE merchant_categ_id = ? AND merchant_id = ?`,
		name, categoryID, merchantID,
	)
	if err != nil {
		return err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

// BulkAssignProductsToCategory assigns multiple products to a category (including their sub-products)
func (r *MenuRepository) BulkAssignProductsToCategory(ctx context.Context, merchantID, categoryID string, productIDs []string) error {
	if len(productIDs) == 0 {
		return fmt.Errorf("product_ids list cannot be empty")
	}

	db := dbx.GetDB(ctx, r.database)

	// 2. Vérifier que la catégorie existe et appartient au merchant
	var categoryExists int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM productcateg WHERE merchant_categ_id = ? AND merchant_id = ? AND enabled = TRUE`,
		categoryID, merchantID,
	).Scan(&categoryExists)
	if err != nil {
		return fmt.Errorf("failed to check category existence: %w", err)
	}
	if categoryExists == 0 {
		return fmt.Errorf("category does not exist or is disabled")
	}

	// 3. Préparer les placeholders et les arguments dans le BON ORDRE
	// Ordre attendu par le SQL : categoryID, merchantID, puis la liste des productIDs
	placeholders := make([]string, len(productIDs))
	for i := range productIDs {
		placeholders[i] = "?"
	}
	inClause := strings.Join(placeholders, ",")

	// Construction du slice d'arguments final
	args := make([]interface{}, 0, len(productIDs)+2)
	args = append(args, categoryID, merchantID)
	for _, id := range productIDs {
		args = append(args, id)
	}

	// UPDATE 1: Produits racines
	query1 := fmt.Sprintf(`
        UPDATE products 
        SET category = ? 
        WHERE merchant_id = ? AND product_id IN (%s) AND enabled = TRUE`,
		inClause)

	_, err = db.ExecContext(ctx, query1, args...)
	if err != nil {
		return fmt.Errorf("failed to update root products: %w", err)
	}

	// UPDATE 2: Sous-produits (by_product_of)
	// On réutilise les mêmes arguments car la structure de la requête est identique
	query2 := fmt.Sprintf(`
        UPDATE products 
        SET category = ? 
        WHERE merchant_id = ? AND by_product_of IN (%s) AND enabled = TRUE`,
		inClause)

	_, err = db.ExecContext(ctx, query2, args...)
	if err != nil {
		return fmt.Errorf("failed to update sub-products: %w", err)
	}

	// Rafraîchir la date de dernière mise à jour
	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

func (r *MenuRepository) DeleteProductCategory(ctx context.Context, merchantID, categoryID string) error {
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx,
		`UPDATE productcateg 
		 SET enabled = FALSE
		 WHERE merchant_categ_id = ? AND merchant_id = ?`,
		categoryID, merchantID,
	)
	if err != nil {
		return err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

func (r *MenuRepository) DeleteComponent(ctx context.Context, merchantID, componentID string) error {
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx,
		`UPDATE components 
		 SET enabled = FALSE
		 WHERE component_id = ? AND merchant_id = ?`,
		componentID, merchantID,
	)
	if err != nil {
		return err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

// ComponentCategoryExists indique si la catégorie existe et est active pour ce
// marchand. Sert à distinguer un 404 d'une suppression silencieuse : l'UPDATE
// de suppression ne remonte pas l'absence de ligne.
func (r *MenuRepository) ComponentCategoryExists(ctx context.Context, merchantID, categoryID string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)

	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM component_category
		 WHERE merchant_categ_id = ? AND merchant_id = ? AND enabled = TRUE`,
		categoryID, merchantID,
	).Scan(&count); err != nil {
		return false, err
	}

	return count > 0, nil
}

// CountComponentsInCategory compte les ingrédients actifs rattachés à la catégorie.
func (r *MenuRepository) CountComponentsInCategory(ctx context.Context, merchantID, categoryID string) (int, error) {
	db := dbx.GetDB(ctx, r.database)

	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM components
		 WHERE category_id = ? AND merchant_id = ? AND enabled = TRUE`,
		categoryID, merchantID,
	).Scan(&count); err != nil {
		return 0, err
	}

	return count, nil
}

// DeleteComponentCategory désactive une catégorie d'ingrédients et traite ses
// ingrédients selon reassignTo :
//   - reassignTo non vide : les ingrédients y sont déplacés puis la catégorie
//     est désactivée ;
//   - reassignTo vide : les ingrédients sont désactivés avec la catégorie (purge).
//
// Les deux écritures sont dans une transaction : sans cela un échec entre les
// deux laissait des ingrédients rattachés à une catégorie désactivée, donc
// absents de GetAllComponents (qui n'émet que les catégories enabled) — ils
// disparaissaient de l'application tout en restant en base.
func (r *MenuRepository) DeleteComponentCategory(ctx context.Context, merchantID, categoryID, reassignTo string) error {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	// la transaction brute ne passe pas par dbx.GetDB : on l'enveloppe pour
	// que le rebind des placeholders s'applique aussi ici
	txx := dbx.Wrap(tx)

	if reassignTo != "" {
		if _, err = txx.ExecContext(ctx,
			`UPDATE components
			 SET category_id = ?
			 WHERE category_id = ? AND merchant_id = ? AND enabled = TRUE`,
			reassignTo, categoryID, merchantID,
		); err != nil {
			return err
		}
	} else {
		if _, err = txx.ExecContext(ctx,
			`UPDATE components
			 SET enabled = FALSE
			 WHERE category_id = ? AND merchant_id = ? AND enabled = TRUE`,
			categoryID, merchantID,
		); err != nil {
			return err
		}
	}

	if _, err = txx.ExecContext(ctx,
		`UPDATE component_category
		 SET enabled = FALSE
		 WHERE merchant_categ_id = ? AND merchant_id = ?`,
		categoryID, merchantID,
	); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

// UpdateComponentCategory renomme une catégorie d'ingrédients.
// Adressage par merchant_categ_id (varchar) comme le reste du domaine
// composants — les catégories marketing, elles, s'adressent par id.
func (r *MenuRepository) UpdateComponentCategory(ctx context.Context, merchantID, categoryID string, payload UpdateComponentCategoryPayload) error {
	if payload.Name == nil {
		return nil
	}

	name := capitalizeFirst(strings.TrimSpace(*payload.Name))
	if name == "" {
		return fmt.Errorf("category_name_required")
	}

	db := dbx.GetDB(ctx, r.database)

	if _, err := db.ExecContext(ctx,
		`UPDATE component_category
		 SET name = ?
		 WHERE merchant_categ_id = ? AND merchant_id = ? AND enabled = TRUE`,
		name, categoryID, merchantID,
	); err != nil {
		return err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

// UpdateComponentCategoriesDisplayOrder réécrit categ_order selon l'ordre du
// tableau reçu. Les catégories créées avant cette fonctionnalité valent toutes
// 999 (constante à la création) : le premier enregistrement les normalise en 1..N.
func (r *MenuRepository) UpdateComponentCategoriesDisplayOrder(ctx context.Context, merchantID string, categoryIDs []string) error {
	if len(categoryIDs) == 0 {
		return nil
	}

	db := dbx.GetDB(ctx, r.database)

	for i, id := range categoryIDs {
		if _, err := db.ExecContext(ctx,
			`UPDATE component_category
			 SET categ_order = ?
			 WHERE merchant_categ_id = ? AND merchant_id = ? AND enabled = TRUE`,
			i+1, id, merchantID,
		); err != nil {
			return err
		}
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

// UpdateComponent met à jour les informations d'un composant
func (r *MenuRepository) UpdateComponent(ctx context.Context, merchantID, componentID string, updates *UpdateComponentPayload) error {
	db := dbx.GetDB(ctx, r.database)

	// Vérifier que le composant existe et appartient au merchant
	var componentExists int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM components WHERE component_id = ? AND merchant_id = ? AND enabled = TRUE`,
		componentID, merchantID,
	).Scan(&componentExists)
	if err != nil {
		return fmt.Errorf("failed to check component existence: %w", err)
	}
	if componentExists == 0 {
		return fmt.Errorf("component does not exist or is disabled")
	}

	// Vérifier les unités de mesure si fournies. unit_of_measure.id est un
	// integer : un ID non numérique valait 0 en MySQL (aucune ligne) — même
	// refus côté Go.
	if updates.PurchaseUnitID != nil && *updates.PurchaseUnitID != "" {
		if !menuNumericID(*updates.PurchaseUnitID) {
			return fmt.Errorf("purchase_unit_id does not exist")
		}
		var unitExists int
		err = db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM unit_of_measure WHERE id = ?`,
			*updates.PurchaseUnitID,
		).Scan(&unitExists)
		if err != nil {
			return fmt.Errorf("failed to check purchase unit: %w", err)
		}
		if unitExists == 0 {
			return fmt.Errorf("purchase_unit_id does not exist")
		}
	}

	// Construire la requête UPDATE dynamiquement
	updateFields := []string{}
	updateArgs := []interface{}{}

	if updates.Name != nil && *updates.Name != "" {
		// Mettre la première lettre en majuscule
		name := capitalizeFirst(strings.TrimSpace(*updates.Name))
		updateFields = append(updateFields, "name = ?")
		updateArgs = append(updateArgs, name)
	}

	if updates.Price != nil {
		updateFields = append(updateFields, "component_price = ?")
		updateArgs = append(updateArgs, *updates.Price)
	}

	if updates.PurchaseCost != nil {
		updateFields = append(updateFields, "purchase_price = ?")
		updateArgs = append(updateArgs, *updates.PurchaseCost)
	}

	if updates.PurchaseCostQty != nil {
		updateFields = append(updateFields, "purchase_price_quantity = ?")
		updateArgs = append(updateArgs, *updates.PurchaseCostQty)
	}

	if updates.PurchaseUnitID != nil && *updates.PurchaseUnitID != "" {
		updateFields = append(updateFields, "purchase_unit_id = ?")
		updateArgs = append(updateArgs, *updates.PurchaseUnitID)
	}

	if updates.ConservationDays != nil {
		updateFields = append(updateFields, "conservation_days = ?")
		updateArgs = append(updateArgs, *updates.ConservationDays)
	}

	if updates.ConservationType != nil && *updates.ConservationType != "" {
		updateFields = append(updateFields, "conservation_type = ?")
		updateArgs = append(updateArgs, *updates.ConservationType)
	}

	if updates.StorageTempMin != nil {
		updateFields = append(updateFields, "storage_temp_min = ?")
		updateArgs = append(updateArgs, *updates.StorageTempMin)
	}

	if updates.StorageTempMax != nil {
		updateFields = append(updateFields, "storage_temp_max = ?")
		updateArgs = append(updateArgs, *updates.StorageTempMax)
	}

	// S'il n'y a rien à mettre à jour, retourner sans erreur
	if len(updateFields) == 0 {
		return nil
	}

	// Ajouter componentID et merchantID pour la clause WHERE
	updateArgs = append(updateArgs, componentID, merchantID)

	// Exécuter la requête UPDATE
	query := fmt.Sprintf(
		`UPDATE components SET %s WHERE component_id = ? AND merchant_id = ? AND enabled = TRUE`,
		strings.Join(updateFields, ", "),
	)

	_, err = db.ExecContext(ctx, query, updateArgs...)
	if err != nil {
		return fmt.Errorf("failed to update component: %w", err)
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

func (r *MenuRepository) CreateComponent(ctx context.Context, p *UpdateComponentPayload) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	// Vérifier que la catégorie existe
	var categoryExists int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM component_category WHERE merchant_categ_id = ? AND merchant_id = ?`,
		p.CategoryID, p.MerchantID,
	).Scan(&categoryExists)
	if err != nil {
		return "0", fmt.Errorf("check category error: %w", err)
	}
	if categoryExists == 0 {
		return "0", fmt.Errorf("category_not_found")
	}

	// Vérifier que l'unité de mesure de vente existe. unit_of_measure.id est
	// un integer : un ID non numérique valait 0 en MySQL (aucune ligne) —
	// même refus côté Go.
	if p.UnitID == nil || !menuNumericID(*p.UnitID) {
		return "0", fmt.Errorf("unit_not_found")
	}
	var unitExists int
	err = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM unit_of_measure WHERE id = ?`,
		p.UnitID,
	).Scan(&unitExists)
	if err != nil {
		return "0", fmt.Errorf("check unit error: %w", err)
	}
	if unitExists == 0 {
		return "0", fmt.Errorf("unit_not_found")
	}

	// Vérifier que l'unité de mesure d'achat existe si fournie
	if p.PurchaseUnitID != nil && *p.PurchaseUnitID != "" {
		if !menuNumericID(*p.PurchaseUnitID) {
			return "0", fmt.Errorf("purchase_unit_id_not_found")
		}
		var purchaseUnitExists int
		err = db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM unit_of_measure WHERE id = ?`,
			*p.PurchaseUnitID,
		).Scan(&purchaseUnitExists)
		if err != nil {
			return "0", fmt.Errorf("check purchase unit error: %w", err)
		}
		if purchaseUnitExists == 0 {
			return "0", fmt.Errorf("purchase_unit_id_not_found")
		}
	}

	// Mettre la première lettre en majuscule
	name := capitalizeFirst(strings.TrimSpace(*p.Name))

	// Vérifier qu'aucun composant actif ne porte déjà ce nom
	var componentNameExists int
	err = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM components WHERE merchant_id = ? AND LOWER(name) = LOWER(?) AND enabled = TRUE`,
		p.MerchantID, name,
	).Scan(&componentNameExists)
	if err != nil {
		return "0", fmt.Errorf("failed to check component name uniqueness: %w", err)
	}
	if componentNameExists > 0 {
		if r.redis == nil {
			return "0", models.ErrComponentNameAlreadyExists
		}
		// Redis actif : le 1er appel pose une clé de confirmation et bloque ;
		// un 2e appel identique (même merchant + même nom) la trouve déjà
		// posée et la création est acceptée malgré le doublon.
		confirmKey := helpers.GetMenuComponentNameConfirmKey(p.MerchantID, name)
		if r.redis.SetNX(ctx, confirmKey, "1", models.MenuNameConfirmTTL) {
			return "0", models.ErrComponentNameAlreadyExistsWithRetry
		}
		r.redis.Delete(ctx, confirmKey)
	}

	// Déterminer les valeurs optionnelles d'achat
	var purchaseCost interface{} = 0
	if p.PurchaseCost != nil {
		purchaseCost = *p.PurchaseCost
	}

	// Conserver l'unité de vente et déduire séparément l'unité d'achat.
	saleUnitOfMeasure := p.UnitID
	purchaseUnitID := p.UnitID
	if p.PurchaseUnitID != nil && *p.PurchaseUnitID != "" {
		purchaseUnitID = p.PurchaseUnitID
	}

	var conservationDays interface{} = nil
	if p.ConservationDays != nil {
		conservationDays = *p.ConservationDays
	}

	conservationType := "froid"
	if p.ConservationType != nil && *p.ConservationType != "" {
		conservationType = *p.ConservationType
	}

	var storageTempMin interface{} = nil
	if p.StorageTempMin != nil {
		storageTempMin = *p.StorageTempMin
	}

	var storageTempMax interface{} = nil
	if p.StorageTempMax != nil {
		storageTempMax = *p.StorageTempMax
	}

	// Insérer le composant
	query := `
		INSERT INTO components (
			merchant_id,
			name,
			category_id,
			component_price,
			unit_of_measure,
			purchase_unit_id,
			purchase_price,
			conservation_days,
			conservation_type,
			storage_temp_min,
			storage_temp_max,
			enabled,
			status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, TRUE, '1')
	`

	id, err := db.InsertReturningID(
		ctx,
		query,
		"component_id",
		p.MerchantID,
		name,
		p.CategoryID,
		p.Price,
		saleUnitOfMeasure,
		purchaseUnitID,
		purchaseCost,
		conservationDays,
		conservationType,
		storageTempMin,
		storageTempMax,
	)
	if err != nil {
		return "0", fmt.Errorf("insert component error: %w", err)
	}

	_ = r.setMenuUpdated(ctx, p.MerchantID)

	return strconv.FormatInt(id, 10), nil
}

func (r *MenuRepository) CreateComponentCategory(ctx context.Context, p *UpsertComponentCategoryPayload) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	name := strings.TrimSpace(p.Name)
	if name == "" {
		return "0", fmt.Errorf("category_name_required")
	}

	// Mettre la première lettre en majuscule
	name = capitalizeFirst(name)

	// Insérer la catégorie. merchant_categ_id est NOT NULL sans défaut (MySQL
	// non-strict insérait '' avant l'UPDATE qui suit) : '' explicite pour la
	// parité Postgres.
	query := `
		INSERT INTO component_category (
			merchant_id,
			merchant_categ_id,
			name,
			categ_order,
			enabled,
			available
		) VALUES (?, '', ?, 999, TRUE, TRUE)
	`

	id, err := db.InsertReturningID(
		ctx,
		query,
		"id",
		p.MerchantID,
		name,
	)
	if err != nil {
		return "0", fmt.Errorf("insert component category error: %w", err)
	}

	// Mettre à jour merchant_categ_id avec l'ID de la catégorie
	// (colonne varchar : formatage Go de l'ID, pgx n'encode pas un int en varchar)
	query = `
		UPDATE component_category
		SET merchant_categ_id = ?
		WHERE merchant_id = ? AND id = ?
	`

	_, err = db.ExecContext(
		ctx,
		query,
		strconv.FormatInt(id, 10),
		p.MerchantID,
		id,
	)
	if err != nil {
		return "0", fmt.Errorf("update merchant_categ_id error: %w", err)
	}

	_ = r.setMenuUpdated(ctx, p.MerchantID)

	return strconv.FormatInt(id, 10), nil
}

func (r *MenuRepository) CreateProductCategory(ctx context.Context, p *CreateProductCategoryPayload) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	name := strings.TrimSpace(p.Name)
	if name == "" {
		return "0", fmt.Errorf("category_name_required")
	}

	// Mettre la première lettre en majuscule
	name = capitalizeFirst(name)

	// Récupérer le prochain categ_order
	var maxOrder int
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(categ_order), 0) FROM productcateg WHERE merchant_id = ?`,
		p.MerchantID,
	).Scan(&maxOrder)
	if err != nil && err != sql.ErrNoRows {
		return "0", fmt.Errorf("get max order error: %w", err)
	}

	// Insérer la catégorie. merchant_categ_id est NOT NULL sans défaut (MySQL
	// non-strict insérait '' avant l'UPDATE qui suit) : '' explicite pour la
	// parité Postgres.
	query := `
		INSERT INTO productcateg (
			merchant_id,
			merchant_categ_id,
			categ_name,
			categ_order,
			enabled,
			available
		) VALUES (?, '', ?, ?, TRUE, TRUE)
	`

	id, err := db.InsertReturningID(
		ctx,
		query,
		"categ_id",
		p.MerchantID,
		name,
		maxOrder+1,
	)
	if err != nil {
		return "0", fmt.Errorf("insert product category error: %w", err)
	}

	// Mise à jour temporaire avant migration
	// (colonne varchar : formatage Go de l'ID, pgx n'encode pas un int en varchar)
	query = `
		UPDATE productcateg
		SET merchant_categ_id = ?
		WHERE merchant_id = ? AND categ_id = ?
	`

	_, err = db.ExecContext(
		ctx,
		query,
		strconv.FormatInt(id, 10),
		p.MerchantID,
		id,
	)

	_ = r.setMenuUpdated(ctx, p.MerchantID)

	return strconv.FormatInt(id, 10), nil
}

// resolveUpdatableTvaID valide un taux de TVA fourni dans une mise à jour et le
// convertit pour une colonne integer. nil (champ absent du payload) laisse le
// taux inchangé via COALESCE. Un taux inexistant ou désactivé est refusé plutôt
// qu'ignoré silencieusement : sans ça l'appelant croirait la TVA enregistrée.
func (r *MenuRepository) resolveUpdatableTvaID(ctx context.Context, id *string, field string) (*int, error) {
	if id == nil {
		return nil, nil
	}

	trimmed := strings.TrimSpace(*id)
	value, err := strconv.Atoi(trimmed)
	if err != nil {
		return nil, fmt.Errorf("%s '%s' does not exist or is disabled", field, trimmed)
	}

	db := dbx.GetDB(ctx, r.database)
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tva_categories WHERE enabled = TRUE AND tva_id = ?`,
		trimmed,
	).Scan(&count); err != nil {
		return nil, fmt.Errorf("failed to validate %s: %w", field, err)
	}
	if count == 0 {
		return nil, fmt.Errorf("%s '%s' does not exist or is disabled", field, trimmed)
	}

	return &value, nil
}

func (r *MenuRepository) UpdateProduct(ctx context.Context, merchantID, productID string, p ProductUpdatePayload) error {
	db := dbx.GetDB(ctx, r.database)

	// Update basic product fields
	query := `
		UPDATE products
		SET 
			name = COALESCE(?, name),
			product_desc = COALESCE(?, product_desc),
			bg_color = COALESCE(?, bg_color),
			production_color = COALESCE(?, production_color),
			category = COALESCE(?, category),
			price = COALESCE(?, price),
			price_take_away = COALESCE(?, price_take_away),
			price_delivery = COALESCE(?, price_delivery),
			by_product_of = ?,
			is_available_on_sno = COALESCE(?, is_available_on_sno),
			enabled = COALESCE(?, enabled),
			status = COALESCE(?, status),
			available_in = COALESCE(?, available_in),
			available_take_away = COALESCE(?, available_take_away),
			available_delivery = COALESCE(?, available_delivery),
			tva_in_id = COALESCE(?, tva_in_id),
			tva_take_away_id = COALESCE(?, tva_take_away_id),
			tva_delivery_id = COALESCE(?, tva_delivery_id)
		WHERE product_id = ? AND merchant_id = ?
	`

	// tva_*_id sont des colonnes integer : on valide puis on convertit en *int.
	// Un *string dans le COALESCE ferait échouer l'inférence de type Postgres
	// face à une colonne integer (cf. même contrainte à la création).
	tvaIn, err := r.resolveUpdatableTvaID(ctx, p.TvaInID, "tva_in_id")
	if err != nil {
		return err
	}
	tvaTakeAway, err := r.resolveUpdatableTvaID(ctx, p.TvaTakeAwayID, "tva_take_away_id")
	if err != nil {
		return err
	}
	tvaDelivery, err := r.resolveUpdatableTvaID(ctx, p.TvaDeliveryID, "tva_delivery_id")
	if err != nil {
		return err
	}

	// by_product_of est un integer nullable : MySQL coerçait '' en 0 ; côté
	// Go, une chaîne vide ou non numérique devient NULL (même effet racine
	// dans les deux dialectes, cf. rapport 29).
	byProductOf := p.ByProductOf
	if byProductOf != nil && !menuNumericID(*byProductOf) {
		byProductOf = nil
	}

	_, err = db.ExecContext(ctx, query,
		p.Name,
		p.Description,
		p.BgColor,
		p.ProductionColor,
		p.CategoryID,
		p.Price,
		p.PriceTakeAway,
		p.PriceDelivery,
		byProductOf,
		p.IsAvailableOnSno,
		p.Enabled,
		p.Status,
		p.AvailableIn,
		p.AvailableTakeAway,
		p.AvailableDelivery,
		tvaIn,
		tvaTakeAway,
		tvaDelivery,
		productID,
		merchantID,
	)
	if err != nil {
		return fmt.Errorf("failed to update product basic fields: %w", err)
	}

	// Sync integration settings (Uber Eats, Deliveroo)
	if err := r.SyncProductIntegrations(ctx, merchantID, productID, p.Integrations); err != nil {
		return fmt.Errorf("failed to sync product integrations: %w", err)
	}

	// Sync configurable attributes
	if len(p.Configuration) > 0 {
		if err := r.SyncProductAttributes(ctx, merchantID, productID, p.Configuration); err != nil {
			return fmt.Errorf("failed to sync product attributes: %w", err)
		}
	}

	// Sync components (composition)
	if len(p.Components) > 0 {
		if err := r.SyncProductComponents(ctx, merchantID, productID, p.Components); err != nil {
			return fmt.Errorf("failed to sync product components: %w", err)
		}
	}

	// Sync tags
	if len(p.Tags) > 0 {
		if err := r.SyncProductTags(ctx, merchantID, productID, p.Tags); err != nil {
			return fmt.Errorf("failed to sync product tags: %w", err)
		}
	}

	// Sync allergens
	if len(p.Allergens) > 0 {
		if err := r.SyncProductAllergens(ctx, merchantID, productID, p.Allergens); err != nil {
			return fmt.Errorf("failed to sync product allergens: %w", err)
		}
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

// SyncProductIntegrations updates the integration settings for a product (Uber Eats, Deliveroo).
// It updates the sync status and price overrides for each integration.
func (r *MenuRepository) SyncProductIntegrations(ctx context.Context, merchantID, productID string, integrations models.ProductIntegrations) error {
	db := dbx.GetDB(ctx, r.database)

	// Ownership check: verify product belongs to merchant
	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM products WHERE product_id = ? AND merchant_id = ?`,
		productID, merchantID,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to verify product ownership: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("product not found for merchant")
	}

	// Process each integration in the list
	// Update Uber Eats integration settings
	if integrations.UberEats.Enabled || integrations.UberEats.PriceOverride != nil {
		updateQuery := `
				UPDATE products
				SET sync_uber_eats = ?
			`
		args := []interface{}{integrations.UberEats.Enabled}

		// Add price override if specified
		if integrations.UberEats.PriceOverride != nil {
			updateQuery += `, price_uber_eats = ?`
			args = append(args, *integrations.UberEats.PriceOverride)
		}

		updateQuery += ` WHERE product_id = ? AND merchant_id = ?`
		args = append(args, productID, merchantID)

		_, err := db.ExecContext(ctx, updateQuery, args...)
		if err != nil {
			return fmt.Errorf("failed to update uber eats integration: %w", err)
		}
	}

	// Update Deliveroo integration settings
	if integrations.Deliveroo.Enabled || integrations.Deliveroo.PriceOverride != nil {
		updateQuery := `
				UPDATE products
				SET sync_deliveroo = ?
			`
		args := []interface{}{integrations.Deliveroo.Enabled}

		// Add price override if specified
		if integrations.Deliveroo.PriceOverride != nil {
			updateQuery += `, price_deliveroo = ?`
			args = append(args, *integrations.Deliveroo.PriceOverride)
		}

		updateQuery += ` WHERE product_id = ? AND merchant_id = ?`
		args = append(args, productID, merchantID)

		_, err := db.ExecContext(ctx, updateQuery, args...)
		if err != nil {
			return fmt.Errorf("failed to update deliveroo integration: %w", err)
		}
	}

	return nil
}

// SyncProductAttributes replaces all configurable attributes for a product in a single transaction.
// It verifies that the product belongs to merchantID before modifying it.
func (r *MenuRepository) SyncProductAttributes(ctx context.Context, merchantID, productID string, attributeIDs []string) error {
	db := dbx.GetDB(ctx, r.database)

	// Ownership check: verify product belongs to merchant
	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM products WHERE product_id = ? AND merchant_id = ?`,
		productID, merchantID,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to verify product ownership: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("product not found for merchant")
	}

	// Delete all existing product_configurable_attribute associations
	_, err = db.ExecContext(ctx,
		`DELETE FROM product_configurable_attribute WHERE product_id = ?`,
		productID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete old attributes: %w", err)
	}

	// Insert new attribute associations with ordering
	for i, attributeID := range attributeIDs {
		_, err := db.ExecContext(ctx,
			`INSERT INTO product_configurable_attribute
			 (product_id, configurable_attribute_id, enabled, num_order)
			 VALUES (?, ?, TRUE, ?)`,
			productID, attributeID, i,
		)
		if err != nil {
			return fmt.Errorf("failed to insert attribute association: %w", err)
		}
	}

	return nil
}

// SyncProductComponents replaces all component requirements for a product in a single transaction.
// It verifies that the product belongs to merchantID before modifying it.
func (r *MenuRepository) SyncProductComponents(ctx context.Context, merchantID, productID string, components []ProductComponentUpdate) error {
	db := dbx.GetDB(ctx, r.database)

	// Ownership check: verify product belongs to merchant
	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM products WHERE product_id = ? AND merchant_id = ?`,
		productID, merchantID,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to verify product ownership: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("product not found for merchant")
	}

	// Get or create recipe for this product
	var recipeID int64
	err = db.QueryRowContext(ctx,
		`SELECT recipe_id FROM recipes WHERE product_id = ? LIMIT 1`,
		productID,
	).Scan(&recipeID)

	if err == sql.ErrNoRows {
		// Create new recipe for this product. merchant_id est NOT NULL sans
		// défaut et n'était jamais renseigné (MySQL non-strict insérait la
		// valeur vide) — on écrit le merchantID réel, disponible ici, dans
		// les deux dialectes.
		recipeID, err = db.InsertReturningID(ctx,
			`INSERT INTO recipes (product_id, merchant_id) VALUES (?, ?)`,
			"recipe_id",
			productID, merchantID,
		)
		if err != nil {
			return fmt.Errorf("failed to create recipe: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to query recipe: %w", err)
	}

	// Delete all existing requires for this recipe
	_, err = db.ExecContext(ctx,
		`DELETE FROM requires WHERE recipe_id = ?`,
		recipeID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete old requires: %w", err)
	}

	// Insert new component requirements. component_id/unit_of_measure sont des
	// integers : un ID non numérique valait 0 en MySQL non-strict — même
	// coercition côté Go (0 = aucune correspondance) pour éviter l'erreur de
	// type Postgres.
	for _, comp := range components {
		componentID := comp.ComponentID
		if !menuNumericID(componentID) {
			componentID = "0"
		}
		unitID := comp.UnitID
		if !menuNumericID(unitID) {
			unitID = "0"
		}
		_, err := db.ExecContext(ctx,
			`INSERT INTO requires
			 (recipe_id, component_id, quantity, unit_of_measure, enabled)
			 VALUES (?, ?, ?, ?, TRUE)`,
			recipeID, componentID, comp.Quantity, unitID,
		)
		if err != nil {
			return fmt.Errorf("failed to insert component requirement: %w", err)
		}
	}

	return nil
}

func (r *MenuRepository) GetProductImageURL(ctx context.Context, merchantID, productID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	var imageURL sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT image_url FROM products
		 WHERE product_id = ? AND merchant_id = ?`,
		productID, merchantID,
	).Scan(&imageURL)

	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil // Produit n'existe pas ou pas d'image
		}
		return "", fmt.Errorf("failed to get product image: %w", err)
	}

	if imageURL.Valid {
		return imageURL.String, nil
	}
	return "", nil
}

func (r *MenuRepository) UpdateProductImage(ctx context.Context, merchantID, productID, imageURL string) error {
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx,
		`UPDATE products 
		 SET image_url = ?
		 WHERE product_id = ? AND merchant_id = ?`,
		imageURL, productID, merchantID,
	)
	if err != nil {
		return fmt.Errorf("failed to update product image: %w", err)
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

func (r *MenuRepository) GetProductCategoryImageURL(ctx context.Context, merchantID, categoryID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	var imageURL sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT image_url FROM productcateg
		 WHERE merchant_categ_id = ? AND merchant_id = ? AND enabled = TRUE`,
		categoryID, merchantID,
	).Scan(&imageURL)

	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("failed to get product category image: %w", err)
	}

	if imageURL.Valid {
		return imageURL.String, nil
	}
	return "", nil
}

func (r *MenuRepository) UpdateProductCategoryImageURL(ctx context.Context, merchantID, categoryID, imageURL string) error {
	db := dbx.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx,
		`UPDATE productcateg
		 SET image_url = ?
		 WHERE merchant_categ_id = ? AND merchant_id = ? AND enabled = TRUE`,
		imageURL, categoryID, merchantID,
	)
	if err != nil {
		return fmt.Errorf("failed to update product category image: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check update result: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("product_category_not_found")
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

func (r *MenuRepository) ClearProductCategoryImageURL(ctx context.Context, merchantID, categoryID string) error {
	db := dbx.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx,
		`UPDATE productcateg
		 SET image_url = NULL
		 WHERE merchant_categ_id = ? AND merchant_id = ? AND enabled = TRUE`,
		categoryID, merchantID,
	)
	if err != nil {
		return fmt.Errorf("failed to clear product category image: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check update result: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("product_category_not_found")
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

func (r *MenuRepository) GetMarketingCategoryImageURL(ctx context.Context, merchantID, categoryID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	var imageURL sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT image_url FROM marketing_categories
		 WHERE id = ? AND merchant_id = ? AND enabled = TRUE`,
		categoryID, merchantID,
	).Scan(&imageURL)

	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("failed to get marketing category image: %w", err)
	}

	if imageURL.Valid {
		return imageURL.String, nil
	}
	return "", nil
}

func (r *MenuRepository) UpdateMarketingCategoryImageURL(ctx context.Context, merchantID, categoryID, imageURL string) error {
	db := dbx.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx,
		`UPDATE marketing_categories
		 SET image_url = ?
		 WHERE id = ? AND merchant_id = ? AND enabled = TRUE`,
		imageURL, categoryID, merchantID,
	)
	if err != nil {
		return fmt.Errorf("failed to update marketing category image: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check update result: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("marketing_category_not_found")
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

func (r *MenuRepository) ClearMarketingCategoryImageURL(ctx context.Context, merchantID, categoryID string) error {
	db := dbx.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx,
		`UPDATE marketing_categories
		 SET image_url = NULL
		 WHERE id = ? AND merchant_id = ? AND enabled = TRUE`,
		categoryID, merchantID,
	)
	if err != nil {
		return fmt.Errorf("failed to clear marketing category image: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check update result: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("marketing_category_not_found")
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

func (r *MenuRepository) setMenuUpdated(ctx context.Context, merchantID string) error {
	db := dbx.GetDB(ctx, r.database)

	query := `
		UPDATE merchant_parameters
		SET last_menu_update = ` + dbx.UTCNow() + `
		WHERE merchant_id = ?
	`

	_, err := db.ExecContext(ctx, query, merchantID)

	return err
}

// SyncProductAllergens replaces all allergen associations for a product in a single transaction.
// It verifies that the product belongs to merchantID before modifying it.
func (r *MenuRepository) SyncProductAllergens(ctx context.Context, merchantID, productID string, allergenIDs []string) error {
	db := dbx.GetDB(ctx, r.database)

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
	db := dbx.GetDB(ctx, r.database)

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

// BulkAssignProductsToTag replaces all product-tag links for a given tag.
// Removes all existing links from this tag to any product, then adds new links to the provided product IDs.
// Ownership of both the tag and every product is verified against merchantID.
func (r *MenuRepository) BulkAssignProductsToTag(ctx context.Context, merchantID, tagID string, productIDs []string) error {
	db := dbx.GetDB(ctx, r.database)

	// 1. Verify that the tag belongs to the merchant
	var tagCount int
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(1) FROM tags WHERE tag_id = ? AND merchant_id = ?",
		tagID, merchantID,
	).Scan(&tagCount)

	if err != nil || tagCount == 0 {
		return models.ErrForbidden
	}

	// 3. Delete all existing product-tag links for this tag
	if _, err := db.ExecContext(ctx,
		"DELETE FROM product_tags WHERE tag_id = ?",
		tagID,
	); err != nil {
		return err
	}

	// 4. If no new products provided, just return
	if len(productIDs) == 0 {
		return nil
	}

	// 5. Deduplicate product IDs
	productIDs = uniqueStrings(productIDs)

	// 6. Verify that ALL products belong to the merchant
	placeholders := make([]string, len(productIDs))
	args := make([]interface{}, len(productIDs)+1)
	args[0] = merchantID

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

	// 7. Insert all new product-tag links
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

func (r *MenuRepository) GetMarketingCategories(ctx context.Context, merchantID string) ([]MarketingCategoryEntry, error) {
	db := dbx.GetDB(ctx, r.database)

	// GROUP_CONCAT est MySQL-only -> string_agg côté Postgres (même tri, même
	// séparateur ; DISTINCT impose que l'ORDER BY porte sur l'expression agrégée)
	concatExpr := `COALESCE(GROUP_CONCAT(DISTINCT pmc.product_id ORDER BY pmc.product_id SEPARATOR ','), '')`
	if dbx.ActiveDialect() == dbx.Postgres {
		concatExpr = `COALESCE(string_agg(DISTINCT pmc.product_id, ',' ORDER BY pmc.product_id), '')`
	}
	query := `
		SELECT
			mc.id,
			mc.name,
			mc.display_order,
			mc.image_url,
			mc.available,
			COUNT(DISTINCT pmc.product_id) AS product_count,
			` + concatExpr + ` AS product_ids
		FROM marketing_categories mc
		LEFT JOIN product_marketing_categories pmc
			ON pmc.marketing_category_id = mc.id
			AND pmc.merchant_id = mc.merchant_id
		WHERE mc.merchant_id = ? AND mc.enabled = TRUE
		GROUP BY mc.id, mc.name, mc.display_order, mc.image_url, mc.available
		ORDER BY mc.display_order ASC, mc.id ASC
	`

	rows, err := db.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []MarketingCategoryEntry{}
	for rows.Next() {
		var row MarketingCategoryEntry
		var imageURL sql.NullString
		var productIDsRaw string
		if err := rows.Scan(&row.CategoryID, &row.Name, &row.DisplayOrder, &imageURL, &row.Available, &row.ProductCount, &productIDsRaw); err != nil {
			return nil, err
		}

		if imageURL.Valid {
			row.ImageURL = &imageURL.String
		}

		row.ProductIDs = []string{}
		if productIDsRaw != "" {
			row.ProductIDs = strings.Split(productIDsRaw, ",")
		}
		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func (r *MenuRepository) CreateMarketingCategory(ctx context.Context, merchantID, name string) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("category_name_required")
	}

	name = capitalizeFirst(name)

	var maxOrder int
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(display_order), 0) FROM marketing_categories WHERE merchant_id = ?`,
		merchantID,
	).Scan(&maxOrder)
	if err != nil {
		return "", fmt.Errorf("get max order error: %w", err)
	}

	// id est une PK varchar générée côté client : l'ancien LastInsertId()
	// renvoyait 0 en MySQL (pas d'auto-increment sur cette table) — la
	// fonction retournait donc "0" au lieu de l'ID réellement créé, et le
	// même appel échoue durement sous pgx. On retourne désormais l'ID généré
	// (bug de prod corrigé, à déployer indépendamment de la migration).
	categoryID := helpers.GeneratePrefixedID("mark-categ")
	_, err = db.ExecContext(ctx,
		`INSERT INTO marketing_categories (id, merchant_id, name, display_order, enabled, available)
		 VALUES (?, ?, ?, ?, TRUE, TRUE)`,
		categoryID, merchantID, name, maxOrder+1,
	)
	if err != nil {
		return "", err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return categoryID, nil
}

func (r *MenuRepository) UpdateMarketingCategory(ctx context.Context, merchantID, categoryID string, payload UpdateMarketingCategoryPayload) error {
	db := dbx.GetDB(ctx, r.database)

	fields := []string{}
	args := []interface{}{}

	if payload.Name != nil {
		name := strings.TrimSpace(*payload.Name)
		if name == "" {
			return fmt.Errorf("category_name_required")
		}
		name = capitalizeFirst(name)
		fields = append(fields, "name = ?")
		args = append(args, name)
	}

	if payload.Available != nil {
		fields = append(fields, "available = ?")
		args = append(args, *payload.Available)
	}

	if len(fields) == 0 {
		return nil
	}

	args = append(args, categoryID, merchantID)
	query := fmt.Sprintf(`UPDATE marketing_categories SET %s WHERE id = ? AND merchant_id = ? AND enabled = TRUE`, strings.Join(fields, ", "))

	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

func (r *MenuRepository) DeleteMarketingCategory(ctx context.Context, merchantID, categoryID string) error {
	db := dbx.GetDB(ctx, r.database)

	if _, err := db.ExecContext(ctx,
		`UPDATE marketing_categories SET enabled = FALSE WHERE id = ? AND merchant_id = ?`,
		categoryID, merchantID,
	); err != nil {
		return err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

func (r *MenuRepository) UpdateMarketingCategoriesDisplayOrder(ctx context.Context, merchantID string, categoryIDs []string) error {
	if len(categoryIDs) == 0 {
		return nil
	}

	db := dbx.GetDB(ctx, r.database)

	for i, id := range categoryIDs {
		if _, err := db.ExecContext(ctx,
			`UPDATE marketing_categories SET display_order = ? WHERE id = ? AND merchant_id = ? AND enabled = TRUE`,
			i+1, id, merchantID,
		); err != nil {
			return err
		}
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

func (r *MenuRepository) AssignProductMarketingCategory(ctx context.Context, merchantID, productID, categoryID string) error {
	db := dbx.GetDB(ctx, r.database)

	var productCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM products WHERE product_id = ? AND merchant_id = ? AND enabled = TRUE`,
		productID, merchantID,
	).Scan(&productCount); err != nil {
		return err
	}
	if productCount == 0 {
		return models.ErrForbidden
	}

	var categoryCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM marketing_categories WHERE id = ? AND merchant_id = ? AND enabled = TRUE`,
		categoryID, merchantID,
	).Scan(&categoryCount); err != nil {
		return err
	}
	if categoryCount == 0 {
		return models.ErrForbidden
	}

	// PK de product_marketing_categories = (product_id) -> ON CONFLICT côté PG
	upsertQuery := `INSERT INTO product_marketing_categories (product_id, marketing_category_id, merchant_id)
		 VALUES (?, ?, ?)
		 ON DUPLICATE KEY UPDATE marketing_category_id = VALUES(marketing_category_id), merchant_id = VALUES(merchant_id), updated_at = CURRENT_TIMESTAMP`
	if dbx.ActiveDialect() == dbx.Postgres {
		upsertQuery = `INSERT INTO product_marketing_categories (product_id, marketing_category_id, merchant_id)
		 VALUES (?, ?, ?)
		 ON CONFLICT (product_id) DO UPDATE SET marketing_category_id = EXCLUDED.marketing_category_id, merchant_id = EXCLUDED.merchant_id, updated_at = CURRENT_TIMESTAMP`
	}
	_, err := db.ExecContext(ctx, upsertQuery, productID, categoryID, merchantID)
	if err != nil {
		return err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

func (r *MenuRepository) UnassignProductMarketingCategory(ctx context.Context, merchantID, productID string) error {
	db := dbx.GetDB(ctx, r.database)

	if _, err := db.ExecContext(ctx,
		`DELETE FROM product_marketing_categories WHERE merchant_id = ? AND product_id = ?`,
		merchantID, productID,
	); err != nil {
		return err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

func (r *MenuRepository) BulkAssignProductsToMarketingCategory(ctx context.Context, merchantID, categoryID string, productIDs []string) error {
	db := dbx.GetDB(ctx, r.database)

	if len(productIDs) == 0 {
		return nil
	}

	productIDs = uniqueStrings(productIDs)

	var categoryCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM marketing_categories WHERE id = ? AND merchant_id = ? AND enabled = TRUE`,
		categoryID, merchantID,
	).Scan(&categoryCount); err != nil {
		return err
	}
	if categoryCount == 0 {
		return models.ErrForbidden
	}

	placeholders := make([]string, len(productIDs))
	args := make([]interface{}, len(productIDs)+1)
	args[0] = merchantID
	for i, pid := range productIDs {
		placeholders[i] = "?"
		args[i+1] = pid
	}

	query := fmt.Sprintf(
		"SELECT COUNT(1) FROM products WHERE merchant_id = ? AND enabled = TRUE AND product_id IN (%s)",
		strings.Join(placeholders, ","),
	)

	var validCount int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&validCount); err != nil {
		return err
	}
	if validCount != len(productIDs) {
		return models.ErrForbidden
	}

	insertValues := make([]string, len(productIDs))
	insertArgs := make([]interface{}, 0, len(productIDs)*3)
	for i, pid := range productIDs {
		insertValues[i] = "(?, ?, ?)"
		insertArgs = append(insertArgs, pid, categoryID, merchantID)
	}

	onDup := `ON DUPLICATE KEY UPDATE marketing_category_id = VALUES(marketing_category_id), merchant_id = VALUES(merchant_id), updated_at = CURRENT_TIMESTAMP`
	if dbx.ActiveDialect() == dbx.Postgres {
		onDup = `ON CONFLICT (product_id) DO UPDATE SET marketing_category_id = EXCLUDED.marketing_category_id, merchant_id = EXCLUDED.merchant_id, updated_at = CURRENT_TIMESTAMP`
	}
	insertQuery := fmt.Sprintf(
		`INSERT INTO product_marketing_categories (product_id, marketing_category_id, merchant_id) VALUES %s
		 %s`,
		strings.Join(insertValues, ","),
		onDup,
	)

	if _, err := db.ExecContext(ctx, insertQuery, insertArgs...); err != nil {
		return err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

// GetMenuWithMarketingCategories returns the menu with marketing category overrides applied.
// Products assigned to a marketing category will appear under that category instead of their
// standard productcateg category. Products without an assignment keep their original category.
// This method is intended for external platform sync (ScanNOrder, Uber Eats, Deliveroo) ONLY.
// GET /menu must always call GetMenu directly.
func (r *MenuRepository) GetMenuWithMarketingCategories(ctx context.Context, merchantID string) (*models.MenuResponse, error) {
	menu, err := r.GetMenu(ctx, merchantID, nil)
	if err != nil {
		return nil, err
	}
	if menu.Status != "ok" || len(menu.ProductsTypes) == 0 {
		return menu, nil
	}

	db := dbx.GetDB(ctx, r.database)

	type assignment struct {
		categoryID   string
		categoryName string
		displayOrder int
		imageURL     *string
	}
	assignments := make(map[string]assignment) // productID → marketing assignment

	rows, err := db.QueryContext(ctx, `
		SELECT pmc.product_id, mc.id, mc.name, mc.display_order, mc.image_url
		FROM product_marketing_categories pmc
		INNER JOIN marketing_categories mc ON mc.id = pmc.marketing_category_id
		WHERE pmc.merchant_id = ? AND mc.enabled = TRUE
	`, merchantID)
	if err != nil {
		// On error fall back to standard categories rather than failing
		return menu, nil
	}
	defer rows.Close()

	for rows.Next() {
		var productID, catID, catName string
		var displayOrder int
		var imageURL sql.NullString
		if err := rows.Scan(&productID, &catID, &catName, &displayOrder, &imageURL); err != nil {
			continue
		}
		var catImageURL *string
		if imageURL.Valid {
			catImageURL = &imageURL.String
		}
		assignments[productID] = assignment{catID, catName, displayOrder, catImageURL}
	}

	if len(assignments) == 0 {
		return menu, nil
	}

	// Build two buckets: marketing-mapped and standard categories
	marketingCatsMap := make(map[string]*models.ProductCategory)
	standardCatsMap := make(map[string]*models.ProductCategory)
	var standardCatOrder []string // preserves original display order

	for _, pt := range menu.ProductsTypes {
		catKey := ""
		if pt.CategoryID != nil {
			catKey = *pt.CategoryID
		}
		for _, p := range pt.Products {
			if asgn, ok := assignments[p.ProductID]; ok {
				if _, exists := marketingCatsMap[asgn.categoryID]; !exists {
					catIDCopy := asgn.categoryID
					marketingCatsMap[asgn.categoryID] = &models.ProductCategory{
						Category:     asgn.categoryName,
						CategoryName: asgn.categoryName,
						CategoryID:   &catIDCopy,
						Order:        asgn.displayOrder,
						ImageURL:     asgn.imageURL,
						Available:    true,
						Products:     []models.ProductEntry{},
					}
				}
				marketingCatsMap[asgn.categoryID].Products = append(marketingCatsMap[asgn.categoryID].Products, p)
			} else {
				if _, exists := standardCatsMap[catKey]; !exists {
					standardCatsMap[catKey] = &models.ProductCategory{
						Category:     pt.Category,
						CategoryName: pt.CategoryName,
						CategoryID:   pt.CategoryID,
						Order:        pt.Order,
						BgColor:      pt.BgColor,
						ImageURL:     pt.ImageURL,
						Available:    pt.Available,
						Products:     []models.ProductEntry{},
					}
					standardCatOrder = append(standardCatOrder, catKey)
				}
				standardCatsMap[catKey].Products = append(standardCatsMap[catKey].Products, p)
			}
		}
	}

	// Sort marketing categories by display_order
	var marketingCats []models.ProductCategory
	for _, mc := range marketingCatsMap {
		if len(mc.Products) > 0 {
			marketingCats = append(marketingCats, *mc)
		}
	}
	sort.Slice(marketingCats, func(i, j int) bool {
		return marketingCats[i].Order < marketingCats[j].Order
	})

	// Marketing categories first, then standard categories in original order
	result := make([]models.ProductCategory, 0, len(marketingCats)+len(standardCatOrder))
	result = append(result, marketingCats...)
	for _, key := range standardCatOrder {
		if stdCat, exists := standardCatsMap[key]; exists && len(stdCat.Products) > 0 {
			result = append(result, *stdCat)
		}
	}

	menu.ProductsTypes = result
	return menu, nil
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
	db := dbx.GetDB(ctx, r.database)

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

	// Upsert associations — INSERT IGNORE est MySQL-only, la forme Postgres
	// est ON CONFLICT DO NOTHING sur la PK (product_id, allergen_id).
	upsertQuery := `INSERT IGNORE INTO product_allergens (product_id, allergen_id) VALUES (?, ?)`
	if dbx.ActiveDialect() == dbx.Postgres {
		upsertQuery = `INSERT INTO product_allergens (product_id, allergen_id) VALUES (?, ?) ON CONFLICT (product_id, allergen_id) DO NOTHING`
	}
	stmt, err := db.PrepareContext(ctx, upsertQuery)
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
	db := dbx.GetDB(ctx, r.database)

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
			`SELECT COUNT(1) FROM tags WHERE merchant_id = ? AND tag_id IN (`+inClause+`)`,
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
	db := dbx.GetDB(ctx, r.database)

	// Ownership check : merchantID était accepté mais jamais vérifié, ce qui
	// permettait de modifier les attributs d'un produit d'un autre marchand.
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

	// 2. Reset Config (Set enabled = 0)
	// Correspond à: UPDATE product_configurable_attribute SET enabled = 0 WHERE product_id = ...
	_, err := db.ExecContext(ctx, `
		UPDATE product_configurable_attribute
		SET enabled = FALSE
		WHERE product_id = ?`, productID)
	if err != nil {
		return err
	}

	// 3. Loop et Upsert
	// Correspond au foreach($product->configuration) en PHP
	// PK = (configurable_attribute_id, product_id) -> ON CONFLICT côté PG ;
	// enabled est boolean en cible.
	stmtQuery := `
		INSERT INTO product_configurable_attribute(product_id, configurable_attribute_id, num_order, enabled)
		VALUES(?, ?, ?, TRUE)
		ON DUPLICATE KEY UPDATE enabled = 1, num_order = VALUES(num_order)
	`
	if dbx.ActiveDialect() == dbx.Postgres {
		stmtQuery = `
		INSERT INTO product_configurable_attribute(product_id, configurable_attribute_id, num_order, enabled)
		VALUES(?, ?, ?, TRUE)
		ON CONFLICT (configurable_attribute_id, product_id) DO UPDATE SET enabled = TRUE, num_order = EXCLUDED.num_order
	`
	}

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
	db := dbx.GetDB(ctx, r.database)

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

	result := make([]models.TagEntry, 0)
	for rows.Next() {
		var t models.TagEntry
		if err := rows.Scan(&t.ID, &t.MerchantID, &t.Name); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

// BulkUpdateProductPrices updates prices for multiple products
func (r *MenuRepository) BulkUpdateProductPrices(ctx context.Context, merchantID string, products []BulkUpdateProductPrice) error {
	db := dbx.GetDB(ctx, r.database)

	for _, product := range products {
		// Verify product belongs to merchant
		var count int
		err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM products WHERE product_id = ? AND merchant_id = ?`,
			product.ProductID, merchantID,
		).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to verify product ownership: %w", err)
		}
		if count == 0 {
			return fmt.Errorf("product %s not found for merchant", product.ProductID)
		}

		// Build dynamic UPDATE query based on provided fields
		updateParts := []string{}
		args := []interface{}{}

		if product.Price != nil {
			updateParts = append(updateParts, "price = ?")
			args = append(args, *product.Price)
		}
		if product.PriceTakeAway != nil {
			updateParts = append(updateParts, "price_take_away = ?")
			args = append(args, *product.PriceTakeAway)
		}
		if product.PriceDelivery != nil {
			updateParts = append(updateParts, "price_delivery = ?")
			args = append(args, *product.PriceDelivery)
		}
		if product.PriceUberEats != nil {
			updateParts = append(updateParts, "price_uber_eats = ?")
			args = append(args, *product.PriceUberEats)
		}
		if product.PriceDeliveroo != nil {
			updateParts = append(updateParts, "price_deliveroo = ?")
			args = append(args, *product.PriceDeliveroo)
		}

		// Skip if no prices provided
		if len(updateParts) == 0 {
			continue
		}

		// Add WHERE clause
		args = append(args, product.ProductID, merchantID)

		query := fmt.Sprintf(
			"UPDATE products SET %s WHERE product_id = ? AND merchant_id = ?",
			strings.Join(updateParts, ", "),
		)

		_, err = db.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("failed to update product %s prices: %w", product.ProductID, err)
		}
	}

	// Update menu modification time
	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

// DeleteProduct disables a product by setting enabled = 0.
// It verifies that the product belongs to merchantID before modifying it.
func (r *MenuRepository) DeleteProduct(ctx context.Context, merchantID, productID string) error {
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx,
		`UPDATE products 
		 SET enabled = FALSE
		 WHERE product_id = ? AND merchant_id = ?`,
		productID, merchantID,
	)
	if err != nil {
		return err
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}

// UpdateDisplayOrder updates both category order and product display order
func (r *MenuRepository) UpdateDisplayOrder(ctx context.Context, merchantID string, payload DisplayOrderPayload) error {
	db := dbx.GetDB(ctx, r.database)

	// Update category orders
	for catOrder, item := range payload.DisplayOrder {
		_, err := db.ExecContext(ctx,
			`UPDATE productcateg 
				 SET categ_order = ?
				 WHERE categ_id = ? AND merchant_id = ? AND enabled = TRUE`,
			catOrder, item.CategoryID, merchantID,
		)
		if err != nil {
			return err
		}

		// Update product display orders within this category
		for prodOrder, productID := range item.Products {
			_, err := db.ExecContext(ctx,
				`UPDATE products 
					 SET display_order = ?
					 WHERE product_id = ? AND merchant_id = ? AND enabled = TRUE`,
				prodOrder, productID, merchantID,
			)
			if err != nil {
				return err
			}
		}
	}

	_ = r.setMenuUpdated(ctx, merchantID)

	return nil
}
