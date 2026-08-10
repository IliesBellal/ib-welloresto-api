package menu

import (
	"context"
	"fmt"
	"strings"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/modules/menu/importer"
)

// LoadImportPreviewLookups rassemble tout l'existant dont la preview a besoin.
//
// Strictement en lecture : la preview est un dry-run, aucune de ces requêtes ne
// doit muter quoi que ce soit. Les données sont ensuite passées telles quelles
// à importer.BuildPreview, qui reste ainsi sans dépendance à la base.
//
// Neuf SELECT séquentiels : le pool est plafonné à une connexion ouverte
// (contrainte d'hébergement, cf. internal/database/mysql.go), toute tentative
// de parallélisation serait illusoire.
func (r *MenuRepository) LoadImportPreviewLookups(ctx context.Context, merchantID, provider string) (importer.PreviewLookups, error) {
	var lookups importer.PreviewLookups

	tvaRates, err := r.loadImportTvaRates(ctx)
	if err != nil {
		return lookups, err
	}
	lookups.TvaRates = tvaRates

	categories, err := r.loadImportExistingCategories(ctx, merchantID)
	if err != nil {
		return lookups, err
	}
	lookups.ExistingCategories = categories

	tags, err := r.loadImportExistingTags(ctx, merchantID)
	if err != nil {
		return lookups, err
	}
	lookups.ExistingTags = tags

	products, err := r.loadImportExistingProducts(ctx, merchantID)
	if err != nil {
		return lookups, err
	}
	lookups.ExistingProducts = products

	attributes, err := r.loadImportExistingAttributes(ctx, merchantID)
	if err != nil {
		return lookups, err
	}
	lookups.ExistingAttributes = attributes

	imported, err := r.loadImportedEntities(ctx, merchantID, provider)
	if err != nil {
		return lookups, err
	}
	lookups.Imported = imported

	return lookups, nil
}

// loadImportTvaRates lit la table globale des taux. tva_categories n'a pas de
// merchant_id : un couple (taux, canal) suffit à désigner un tva_id.
//
// delivery_type est un varchar portant 'IN', 'TAKE_AWAY' ou 'DELIVERY'. Le
// commentaire SQL de la colonne annonce des valeurs numériques (« 0 => in,
// 1 => delivery, 3 => take away ») : il est faux, et l'avoir suivi rendait
// toutes les lignes illisibles, donc tous les taux non résolus.
func (r *MenuRepository) loadImportTvaRates(ctx context.Context) ([]importer.TvaRateRow, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT tva_id, delivery_type, tva_rate
		 FROM tva_categories
		 WHERE enabled = TRUE
		 ORDER BY tva_id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load tva rates for import preview: %w", err)
	}
	defer rows.Close()

	var out []importer.TvaRateRow
	for rows.Next() {
		var (
			tvaID        int
			deliveryType string
			rate         float64
		)
		if err := rows.Scan(&tvaID, &deliveryType, &rate); err != nil {
			return nil, fmt.Errorf("failed to scan tva rate: %w", err)
		}

		channel := importer.TvaChannel(strings.ToUpper(strings.TrimSpace(deliveryType)))
		if !channel.IsKnown() {
			// Un delivery_type hors des trois canaux de vente ne concerne pas
			// l'import : on l'écarte plutôt que d'échouer toute la preview.
			continue
		}

		out = append(out, importer.TvaRateRow{
			TvaID:   tvaID,
			Channel: channel,
			Rate:    rate,
		})
	}
	return out, rows.Err()
}

// loadImportExistingCategories lit les catégories caisse actives du marchand.
// merchant_categ_id est renvoyé en plus de la PK : c'est lui que products.category
// référence, et donc lui qui sert au rattachement en cas de réutilisation.
func (r *MenuRepository) loadImportExistingCategories(ctx context.Context, merchantID string) ([]importer.ExistingCategory, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT categ_id, merchant_categ_id, categ_name
		 FROM productcateg
		 WHERE merchant_id = ? AND enabled = TRUE
		 ORDER BY categ_id ASC`,
		merchantID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load categories for import preview: %w", err)
	}
	defer rows.Close()

	var out []importer.ExistingCategory
	for rows.Next() {
		var category importer.ExistingCategory
		if err := rows.Scan(&category.CategID, &category.MerchantCategID, &category.Name); err != nil {
			return nil, fmt.Errorf("failed to scan category: %w", err)
		}
		out = append(out, category)
	}
	return out, rows.Err()
}

// loadImportExistingTags lit les tags du marchand. La table tags ne porte pas
// de colonne enabled : la suppression y est physique.
func (r *MenuRepository) loadImportExistingTags(ctx context.Context, merchantID string) ([]importer.ExistingTag, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT tag_id, name
		 FROM tags
		 WHERE merchant_id = ?
		 ORDER BY tag_id ASC`,
		merchantID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load tags for import preview: %w", err)
	}
	defer rows.Close()

	var out []importer.ExistingTag
	for rows.Next() {
		var tag importer.ExistingTag
		if err := rows.Scan(&tag.TagID, &tag.Name); err != nil {
			return nil, fmt.Errorf("failed to scan tag: %w", err)
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

// loadImportExistingProducts alimente la détection de collision de nom. Le
// filtre enabled = TRUE reprend celui de la validation d'unicité du chemin
// unitaire : un produit supprimé logiquement ne bloque pas un homonyme.
func (r *MenuRepository) loadImportExistingProducts(ctx context.Context, merchantID string) ([]importer.ExistingProduct, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT product_id, name
		 FROM products
		 WHERE merchant_id = ? AND enabled = TRUE
		 ORDER BY product_id ASC`,
		merchantID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load products for import preview: %w", err)
	}
	defer rows.Close()

	var out []importer.ExistingProduct
	for rows.Next() {
		var product importer.ExistingProduct
		if err := rows.Scan(&product.ProductID, &product.Name); err != nil {
			return nil, fmt.Errorf("failed to scan product: %w", err)
		}
		out = append(out, product)
	}
	return out, rows.Err()
}

// loadImportExistingAttributes lit les groupes d'options actifs du marchand.
//
// Seul l'identifiant sert : il permet de savoir si une correspondance d'import
// désigne encore un groupe existant, ou pointe dans le vide parce qu'il a été
// supprimé depuis.
func (r *MenuRepository) loadImportExistingAttributes(ctx context.Context, merchantID string) ([]importer.ExistingAttribute, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT id
		 FROM configurable_attributes
		 WHERE merchant_id = ? AND enabled = TRUE`,
		merchantID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load attributes for import preview: %w", err)
	}
	defer rows.Close()

	var out []importer.ExistingAttribute
	for rows.Next() {
		var attribute importer.ExistingAttribute
		if err := rows.Scan(&attribute.AttributeID); err != nil {
			return nil, fmt.Errorf("failed to scan attribute: %w", err)
		}
		out = append(out, attribute)
	}
	return out, rows.Err()
}

// loadImportedEntities lit les quatre tables de mapping utiles à la preview.
//
// Aucun filtre sur enabled, volontairement : c'est la sémantique retenue à la
// création des tables (migration 080). Un mapping désactivé continue de
// signaler que l'entité a déjà été importée, ce qui évite de ressusciter en
// doublon quelque chose que le marchand a supprimé côté Wello.
//
// import_attribute_options_mapping n'est pas lue : les options n'existent qu'à
// travers leur groupe, dont le sort décide du leur.
func (r *MenuRepository) loadImportedEntities(ctx context.Context, merchantID, provider string) (importer.ImportedEntities, error) {
	imported := importer.ImportedEntities{
		Products:   make(map[string]int),
		Categories: make(map[string]int),
		Tags:       make(map[string]string),
		Attributes: make(map[string]string),
	}

	if err := r.scanIntMapping(ctx, "import_products_mapping", merchantID, provider, imported.Products); err != nil {
		return imported, err
	}
	if err := r.scanIntMapping(ctx, "import_categories_mapping", merchantID, provider, imported.Categories); err != nil {
		return imported, err
	}
	if err := r.scanStringMapping(ctx, "import_tags_mapping", merchantID, provider, imported.Tags); err != nil {
		return imported, err
	}
	if err := r.scanStringMapping(ctx, "import_attributes_mapping", merchantID, provider, imported.Attributes); err != nil {
		return imported, err
	}

	return imported, nil
}

// scanIntMapping lit une table de mapping dont wello_id est un entier
// (products, productcateg). Le nom de table vient d'un littéral appelant, pas
// d'une entrée utilisateur.
func (r *MenuRepository) scanIntMapping(ctx context.Context, table, merchantID, provider string, dest map[string]int) error {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT external_id, wello_id FROM `+table+` WHERE merchant_id = ? AND provider = ?`,
		merchantID, provider,
	)
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			externalID string
			welloID    int
		)
		if err := rows.Scan(&externalID, &welloID); err != nil {
			return fmt.Errorf("failed to scan %s: %w", table, err)
		}
		dest[externalID] = welloID
	}
	return rows.Err()
}

// scanStringMapping lit une table de mapping dont wello_id est un identifiant
// préfixé (tags, configurable_attributes).
func (r *MenuRepository) scanStringMapping(ctx context.Context, table, merchantID, provider string, dest map[string]string) error {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx,
		`SELECT external_id, wello_id FROM `+table+` WHERE merchant_id = ? AND provider = ?`,
		merchantID, provider,
	)
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var externalID, welloID string
		if err := rows.Scan(&externalID, &welloID); err != nil {
			return fmt.Errorf("failed to scan %s: %w", table, err)
		}
		dest[externalID] = welloID
	}
	return rows.Err()
}
