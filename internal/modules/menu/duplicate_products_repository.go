package menu

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

// DuplicatedProduct relie une copie à son produit source.
type DuplicatedProduct struct {
	SourceID string
	NewID    string
	// Root est faux pour un sous-produit recopié avec son groupe : seules les
	// copies racines correspondent à la sélection de l'utilisateur.
	Root bool
	// SourceImageURL est l'image de la source. La copie est créée sans image :
	// c'est à l'appelant de lui en donner une à elle (cf.
	// MenuHandler.duplicateProductImages), jamais de partager celle-ci.
	SourceImageURL string
}

// duplicatedProductColumns sont les colonnes de products recopiées telles
// quelles. En sont exclues : product_id et creation_date (générés),
// by_product_of, category, status et display_order (fixés par
// duplicateProductTx), enabled (défaut TRUE) et image_url (voir
// DuplicatedProduct.SourceImageURL).
const duplicatedProductColumns = `merchant_id, name, product_desc, img, bg_color, production_color,
	display_order, price, price_take_away, price_delivery, price_uber_eats, price_deliveroo,
	available_in, available_take_away, available_delivery,
	tva_in_id, tva_delivery_id, tva_take_away_id, category, status,
	is_product_group, is_available_on_sno, is_available_on_kiosk,
	sync_deliveroo, sync_uber_eats, available, is_popular`

// productLinkCopy décrit une table d'association recopiée d'un produit vers sa
// copie. read est filtrée par le produit source ; write reçoit, dans l'ordre,
// la clé générée par newKey (si présente), le produit copié, les colonnes lues
// par read, puis la date courante si withNow.
//
// Lecture puis VALUES plutôt qu'un INSERT ... SELECT ? : Postgres type un
// paramètre placé dans une liste SELECT en text, ce qu'il refuse ensuite sur
// les colonnes product_id de type integer (availabilities_products,
// discounts_products).
type productLinkCopy struct {
	read    string
	write   string
	newKey  func() interface{}
	withNow bool
}

// productLinkCopies couvre tout ce qui configure un produit. N'en font
// volontairement pas partie : les correspondances Uber Eats / Deliveroo
// (identifiants externes propres au produit d'origine), l'historique (ventes,
// mouvements de stock, notes, suggestions d'upsell apprises), et la
// composition, recopiée à part (copyProductRecipesTx) car elle passe par une
// recette intermédiaire.
var productLinkCopies = []productLinkCopy{
	{
		read:  `SELECT configurable_attribute_id, num_order FROM product_configurable_attribute WHERE product_id = ? AND enabled = TRUE`,
		write: `INSERT INTO product_configurable_attribute (product_id, configurable_attribute_id, num_order, enabled) VALUES (?, ?, ?, TRUE)`,
	},
	{
		read:  `SELECT tag_id FROM product_tags WHERE product_id = ?`,
		write: `INSERT INTO product_tags (product_id, tag_id) VALUES (?, ?)`,
	},
	{
		read:  `SELECT allergen_id FROM product_allergens WHERE product_id = ?`,
		write: `INSERT INTO product_allergens (product_id, allergen_id) VALUES (?, ?)`,
	},
	{
		read:  `SELECT marketing_category_id, merchant_id FROM product_marketing_categories WHERE product_id = ?`,
		write: `INSERT INTO product_marketing_categories (product_id, marketing_category_id, merchant_id) VALUES (?, ?, ?)`,
	},
	{
		// Profils de production : sans eux la copie ne partirait pas en cuisine.
		read:  `SELECT production_profile_id, should_produce, should_monitor FROM product_production_profiles WHERE product_id = ?`,
		write: `INSERT INTO product_production_profiles (product_id, production_profile_id, should_produce, should_monitor) VALUES (?, ?, ?, ?)`,
	},
	{
		// Créneaux de disponibilité. creation_date est écrit explicitement,
		// comme dans le module availabilities.
		read:    `SELECT availability_id FROM availabilities_products WHERE product_id = ? AND enabled = TRUE`,
		write:   `INSERT INTO availabilities_products (availability_product_id, product_id, availability_id, creation_date) VALUES (?, ?, ?, ?)`,
		newKey:  func() interface{} { return helpers.GeneratePrefixedID(helpers.AvailabilityProductPrefix) },
		withNow: true,
	},
	{
		read:  `SELECT discount_id, discount_id_new, new_price FROM discounts_products WHERE product_id = ? AND enabled = TRUE`,
		write: `INSERT INTO discounts_products (product_id, discount_id, discount_id_new, new_price, enabled) VALUES (?, ?, ?, ?, TRUE)`,
	},
	{
		read:  `SELECT discount_id, discount_id_new, option_id, new_price, is_option_mandatory FROM discounts_products_options WHERE product_id = ?`,
		write: `INSERT INTO discounts_products_options (product_id, discount_id, discount_id_new, option_id, new_price, is_option_mandatory) VALUES (?, ?, ?, ?, ?, ?)`,
	},
	{
		read:   `SELECT loyalty_program_id FROM customer_loyalty_program_target_products WHERE product_id = ?`,
		write:  `INSERT INTO customer_loyalty_program_target_products (id, product_id, loyalty_program_id) VALUES (?, ?, ?)`,
		newKey: func() interface{} { return helpers.GeneratePrefixedID("target-prod") },
	},
	{
		read:   `SELECT loyalty_program_id FROM customer_loyalty_program_reward_products WHERE product_id = ?`,
		write:  `INSERT INTO customer_loyalty_program_reward_products (id, product_id, loyalty_program_id) VALUES (?, ?, ?)`,
		newKey: func() interface{} { return helpers.GeneratePrefixedID("reward-prod") },
	},
}

// duplicateTarget décrit où et comment placer une copie.
type duplicateTarget struct {
	categoryID   string
	status       *string // nil = statut de la source
	displayOrder *int    // nil = ordre de la source (sous-produit : ordre dans son groupe)
	parentID     string  // groupe de la copie ; "" = produit racine
}

// DuplicateProducts copie intégralement des produits dans une catégorie caisse,
// dans une seule transaction : un échec n'y laisse aucune copie partielle.
//
// Les copies gardent le nom de leur source (la catégorie suffit à les
// distinguer) et prennent place à la fin de la catégorie cible, dans l'ordre
// des sources. Un groupe est recopié avec ses sous-produits ; un sous-produit
// sélectionné en même temps que son groupe n'est donc pas recopié une seconde
// fois, alors que sélectionné seul il devient un produit indépendant.
//
// Toutes les sources doivent appartenir au marchand, sinon rien n'est copié.
func (r *MenuRepository) DuplicateProducts(ctx context.Context, merchantID, categoryID, status string, productIDs []string) ([]DuplicatedProduct, error) {
	ids := make([]string, 0, len(productIDs))
	seen := make(map[string]bool, len(productIDs))
	for _, id := range productIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("product_ids list cannot be empty")
	}

	var copies []DuplicatedProduct
	err := dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.database)

		var categoryExists int
		if err := db.QueryRowContext(txCtx,
			`SELECT COUNT(*) FROM productcateg WHERE merchant_categ_id = ? AND merchant_id = ? AND enabled = TRUE`,
			categoryID, merchantID,
		).Scan(&categoryExists); err != nil {
			return fmt.Errorf("failed to check category existence: %w", err)
		}
		if categoryExists == 0 {
			return fmt.Errorf("category does not exist or is disabled")
		}

		type source struct {
			id       string
			parentID string
			isGroup  bool
		}
		inClause, idArgs := bulkProductPlaceholders(ids)
		args := append([]interface{}{merchantID}, idArgs...)
		rows, err := db.QueryContext(txCtx, fmt.Sprintf(`
			SELECT product_id, COALESCE(by_product_of, 0), is_product_group
			FROM products
			WHERE merchant_id = ? AND product_id IN (%s) AND enabled = TRUE
			ORDER BY display_order, product_id`, inClause), args...)
		if err != nil {
			return err
		}
		var sources []source
		for rows.Next() {
			var s source
			var parentID int64
			if err := rows.Scan(&s.id, &parentID, &s.isGroup); err != nil {
				rows.Close()
				return err
			}
			if parentID > 0 {
				s.parentID = strconv.FormatInt(parentID, 10)
			}
			sources = append(sources, s)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(sources) != len(ids) {
			return models.ErrForbidden
		}

		var nextOrder int
		if err := db.QueryRowContext(txCtx, `
			SELECT COALESCE(MAX(display_order), -1) + 1
			FROM products
			WHERE merchant_id = ? AND category = ? AND enabled = TRUE
			  AND (by_product_of IS NULL OR by_product_of = 0)`,
			merchantID, categoryID,
		).Scan(&nextOrder); err != nil {
			return err
		}

		for _, src := range sources {
			if src.parentID != "" && seen[src.parentID] {
				continue
			}

			order := nextOrder
			nextOrder++
			root, err := r.duplicateProductTx(txCtx, merchantID, src.id, duplicateTarget{
				categoryID:   categoryID,
				status:       &status,
				displayOrder: &order,
			})
			if err != nil {
				return err
			}
			root.Root = true
			copies = append(copies, root)

			if !src.isGroup {
				continue
			}
			subIDs, err := r.activeSubProductIDsTx(txCtx, merchantID, src.id)
			if err != nil {
				return err
			}
			for _, subID := range subIDs {
				sub, err := r.duplicateProductTx(txCtx, merchantID, subID, duplicateTarget{
					categoryID: categoryID,
					parentID:   root.NewID,
				})
				if err != nil {
					return err
				}
				copies = append(copies, sub)
			}
		}

		if err := r.addCopiesToPrinterFiltersTx(txCtx, merchantID, copies); err != nil {
			return err
		}

		_ = r.setMenuUpdated(txCtx, merchantID)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return copies, nil
}

// duplicateProductTx crée la copie d'un produit et de tout ce qui le configure.
// Doit s'exécuter dans la transaction portée par ctx.
func (r *MenuRepository) duplicateProductTx(ctx context.Context, merchantID, sourceID string, target duplicateTarget) (DuplicatedProduct, error) {
	db := dbx.GetDB(ctx, r.database)

	var imageURL sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT image_url FROM products WHERE product_id = ? AND merchant_id = ?`,
		sourceID, merchantID,
	).Scan(&imageURL); err != nil {
		return DuplicatedProduct{}, fmt.Errorf("failed to read product %s: %w", sourceID, err)
	}

	id, err := db.InsertReturningID(ctx, `
		INSERT INTO products (`+duplicatedProductColumns+`)
		SELECT `+duplicatedProductColumns+`
		FROM products WHERE product_id = ? AND merchant_id = ?`,
		"product_id", sourceID, merchantID,
	)
	if err != nil {
		return DuplicatedProduct{}, fmt.Errorf("failed to copy product %s: %w", sourceID, err)
	}
	newID := strconv.FormatInt(id, 10)

	sets := []string{"category = ?"}
	args := []interface{}{target.categoryID}
	if target.status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *target.status)
	}
	if target.displayOrder != nil {
		sets = append(sets, "display_order = ?")
		args = append(args, *target.displayOrder)
	}
	if target.parentID != "" {
		sets = append(sets, "by_product_of = ?")
		args = append(args, target.parentID)
	}
	args = append(args, newID, merchantID)
	if _, err := db.ExecContext(ctx,
		`UPDATE products SET `+strings.Join(sets, ", ")+` WHERE product_id = ? AND merchant_id = ?`,
		args...,
	); err != nil {
		return DuplicatedProduct{}, fmt.Errorf("failed to place product copy %s: %w", newID, err)
	}

	for _, link := range productLinkCopies {
		if err := copyLinkedRowsTx(ctx, db, link, []interface{}{sourceID}, newID); err != nil {
			return DuplicatedProduct{}, fmt.Errorf("failed to copy product %s links: %w", sourceID, err)
		}
	}

	if err := r.copyProductRecipesTx(ctx, merchantID, sourceID, newID); err != nil {
		return DuplicatedProduct{}, err
	}

	return DuplicatedProduct{SourceID: sourceID, NewID: newID, SourceImageURL: imageURL.String}, nil
}

// copyLinkedRowsTx recopie les lignes d'une table d'association vers la copie.
// Les lignes sont toutes lues avant la première écriture : le driver MySQL
// refuse d'exécuter une requête sur une connexion dont un résultat est encore
// ouvert, ce qui est le cas dans une transaction.
func copyLinkedRowsTx(ctx context.Context, db *dbx.DB, link productLinkCopy, readArgs []interface{}, newID string) error {
	rows, err := db.QueryContext(ctx, link.read, readArgs...)
	if err != nil {
		return err
	}
	columns, err := rows.Columns()
	if err != nil {
		rows.Close()
		return err
	}
	var values [][]interface{}
	for rows.Next() {
		row := make([]interface{}, len(columns))
		ptrs := make([]interface{}, len(columns))
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			rows.Close()
			return err
		}
		values = append(values, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	now := time.Now().UTC()
	for _, row := range values {
		args := make([]interface{}, 0, len(row)+3)
		if link.newKey != nil {
			args = append(args, link.newKey())
		}
		args = append(args, newID)
		args = append(args, row...)
		if link.withNow {
			args = append(args, now)
		}
		if _, err := db.ExecContext(ctx, link.write, args...); err != nil {
			return err
		}
	}
	return nil
}

// copyProductRecipesTx recopie la composition : chaque recette de la source
// devient une recette de la copie, avec ses lignes d'ingrédients actives.
func (r *MenuRepository) copyProductRecipesTx(ctx context.Context, merchantID, sourceID, newID string) error {
	db := dbx.GetDB(ctx, r.database)

	type recipe struct {
		id              int64
		preparationTime int64
	}
	rows, err := db.QueryContext(ctx,
		`SELECT recipe_id, preparation_time FROM recipes WHERE product_id = ?`, sourceID)
	if err != nil {
		return fmt.Errorf("failed to read recipes of product %s: %w", sourceID, err)
	}
	var recipes []recipe
	for rows.Next() {
		var rc recipe
		if err := rows.Scan(&rc.id, &rc.preparationTime); err != nil {
			rows.Close()
			return err
		}
		recipes = append(recipes, rc)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, rc := range recipes {
		newRecipeID, err := db.InsertReturningID(ctx,
			`INSERT INTO recipes (product_id, merchant_id, preparation_time) VALUES (?, ?, ?)`,
			"recipe_id", newID, merchantID, rc.preparationTime,
		)
		if err != nil {
			return fmt.Errorf("failed to copy recipe of product %s: %w", sourceID, err)
		}
		if err := copyLinkedRowsTx(ctx, db, productLinkCopy{
			read: `SELECT component_id, consumable_id, quantity, unit_of_measure, in_orders, take_away_orders, delivery_orders
				FROM requires WHERE recipe_id = ? AND enabled = TRUE`,
			write: `INSERT INTO requires (recipe_id, component_id, consumable_id, quantity, unit_of_measure, in_orders, take_away_orders, delivery_orders, enabled)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, TRUE)`,
		}, []interface{}{rc.id}, strconv.FormatInt(newRecipeID, 10)); err != nil {
			return fmt.Errorf("failed to copy composition of product %s: %w", sourceID, err)
		}
	}
	return nil
}

// activeSubProductIDsTx liste les sous-produits actifs d'un groupe, dans leur
// ordre d'affichage.
func (r *MenuRepository) activeSubProductIDsTx(ctx context.Context, merchantID, groupID string) ([]string, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
		SELECT product_id FROM products
		WHERE merchant_id = ? AND by_product_of = ? AND enabled = TRUE
		ORDER BY display_order, product_id`,
		merchantID, groupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// addCopiesToPrinterFiltersTx ajoute chaque copie aux imprimantes de
// production qui filtrent déjà sa source (printers.production_product_ids,
// tableau JSON d'IDs). Une imprimante sans filtre imprime déjà tout.
func (r *MenuRepository) addCopiesToPrinterFiltersTx(ctx context.Context, merchantID string, copies []DuplicatedProduct) error {
	if len(copies) == 0 {
		return nil
	}
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT printer_id, production_product_ids FROM printers WHERE merchant_id = ? AND production_product_ids IS NOT NULL`,
		merchantID,
	)
	if err != nil {
		return fmt.Errorf("failed to read printer filters: %w", err)
	}
	type printerFilter struct {
		id         string
		productIDs []string
	}
	var filters []printerFilter
	for rows.Next() {
		var f printerFilter
		var raw string
		if err := rows.Scan(&f.id, &raw); err != nil {
			rows.Close()
			return err
		}
		// Même tolérance que le module printers : un filtre illisible est
		// traité comme vide, donc laissé tel quel.
		if json.Unmarshal([]byte(raw), &f.productIDs) != nil || len(f.productIDs) == 0 {
			continue
		}
		filters = append(filters, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, f := range filters {
		filtered := make(map[string]bool, len(f.productIDs))
		for _, id := range f.productIDs {
			filtered[id] = true
		}
		updated := f.productIDs
		for _, c := range copies {
			if filtered[c.SourceID] {
				updated = append(updated, c.NewID)
			}
		}
		if len(updated) == len(f.productIDs) {
			continue
		}
		encoded, err := json.Marshal(updated)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx,
			`UPDATE printers SET production_product_ids = ? WHERE printer_id = ? AND merchant_id = ?`,
			string(encoded), f.id, merchantID,
		); err != nil {
			return fmt.Errorf("failed to update printer filter: %w", err)
		}
	}
	return nil
}

// SetProductImages attache une image à plusieurs produits en un seul passage
// (images des copies, une fois dupliquées sur R2).
func (r *MenuRepository) SetProductImages(ctx context.Context, merchantID string, images map[string]string) error {
	if len(images) == 0 {
		return nil
	}
	return dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.database)
		for productID, imageURL := range images {
			if _, err := db.ExecContext(txCtx,
				`UPDATE products SET image_url = ? WHERE product_id = ? AND merchant_id = ?`,
				imageURL, productID, merchantID,
			); err != nil {
				return fmt.Errorf("failed to update product image: %w", err)
			}
		}
		_ = r.setMenuUpdated(txCtx, merchantID)
		return nil
	})
}
