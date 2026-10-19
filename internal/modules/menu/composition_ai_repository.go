package menu

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/importutil"
	"welloresto-api/internal/modules/menu/importer"
	"welloresto-api/internal/utils/dbutils"
)

// compositionProduct est un produit visé par la composition par IA, avec sa
// composition actuelle (lignes d'ingrédient actives uniquement).
type compositionProduct struct {
	ID          string
	Name        string
	Description string
	IsGroup     bool
	Lines       []compositionLine
}

// compositionLine est une ligne d'ingrédient de requires. Les lignes de
// consommable (consumable_id) ne sont ni lues ni touchées.
type compositionLine struct {
	RequireID   string
	ComponentID string
	Name        string
	Quantity    float64
	UnitID      string
}

// compositionComponent est un ingrédient actif du marchand.
type compositionComponent struct {
	ID     string
	Name   string
	UnitID string
}

// LoadCompositionProducts lit les produits actifs du marchand parmi ids, et
// leur composition. Un identifiant d'un autre marchand, ou désactivé, est
// simplement absent du résultat.
func (r *MenuRepository) LoadCompositionProducts(ctx context.Context, merchantID string, ids []string) ([]compositionProduct, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	db := dbx.GetDB(ctx, r.database)
	inClause, idArgs := bulkProductPlaceholders(ids)

	args := append([]interface{}{merchantID}, idArgs...)
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT product_id::text, name, COALESCE(product_desc, ''), COALESCE(is_product_group, FALSE)
		FROM products
		WHERE merchant_id = ? AND enabled = TRUE AND product_id IN (%s)
		ORDER BY product_id`, inClause), args...)
	if err != nil {
		return nil, fmt.Errorf("composition IA : lecture des produits: %w", err)
	}
	var products []compositionProduct
	index := map[string]int{}
	for rows.Next() {
		var p compositionProduct
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.IsGroup); err != nil {
			rows.Close()
			return nil, err
		}
		index[p.ID] = len(products)
		products = append(products, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	lineArgs := append(append([]interface{}{}, idArgs...), merchantID)
	lines, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT rc.product_id::text, rq.id::text, c.component_id::text, c.name, rq.quantity, rq.unit_of_measure::text
		FROM recipes rc
		JOIN requires rq ON rq.recipe_id = rc.recipe_id
		JOIN components c ON c.component_id = rq.component_id
		WHERE rc.product_id IN (%s) AND c.merchant_id = ? AND rq.enabled = TRUE AND c.enabled = TRUE
		ORDER BY rq.id`, inClause), lineArgs...)
	if err != nil {
		return nil, fmt.Errorf("composition IA : lecture des compositions: %w", err)
	}
	defer lines.Close()
	for lines.Next() {
		var productID string
		var l compositionLine
		if err := lines.Scan(&productID, &l.RequireID, &l.ComponentID, &l.Name, &l.Quantity, &l.UnitID); err != nil {
			return nil, err
		}
		if i, ok := index[productID]; ok {
			products[i].Lines = append(products[i].Lines, l)
		}
	}
	return products, lines.Err()
}

// LoadCompositionComponents lit les ingrédients actifs du marchand.
func (r *MenuRepository) LoadCompositionComponents(ctx context.Context, merchantID string) ([]compositionComponent, error) {
	rows, err := dbx.GetDB(ctx, r.database).QueryContext(ctx, `
		SELECT component_id::text, name, unit_of_measure::text
		FROM components WHERE merchant_id = ? AND enabled = TRUE
		ORDER BY component_id`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("composition IA : lecture des ingrédients: %w", err)
	}
	defer rows.Close()
	var out []compositionComponent
	for rows.Next() {
		var c compositionComponent
		if err := rows.Scan(&c.ID, &c.Name, &c.UnitID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ApplyAICompositions écrit, dans une seule transaction, les compositions
// choisies en relecture : ingrédients et catégories d'ingrédient manquants
// créés (un ingrédient existant de même IngredientKey est réutilisé), puis
// lignes ajoutées ou retirées selon le mode de chaque produit
// (planCompositionChanges). Une ligne gardée n'est pas réécrite : sa quantité
// et ses canaux restent ceux saisis par le restaurateur.
func (r *MenuRepository) ApplyAICompositions(ctx context.Context, merchantID string, items []AICompositionApplyProduct, unitIDs map[string]string) (*AICompositionApplyResult, error) {
	result := &AICompositionApplyResult{}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ProductID)
	}

	err := dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		if err := r.bulkOwnedProductIDsCheck(txCtx, merchantID, ids); err != nil {
			return err
		}
		products, err := r.LoadCompositionProducts(txCtx, merchantID, ids)
		if err != nil {
			return err
		}
		byID := make(map[string]compositionProduct, len(products))
		for _, p := range products {
			byID[p.ID] = p
		}

		resolver, err := r.newComponentResolver(txCtx, merchantID, unitIDs)
		if err != nil {
			return err
		}

		db := dbx.GetDB(txCtx, r.database)
		for _, it := range items {
			product, ok := byID[it.ProductID]
			if !ok {
				// Produit désactivé entre l'aperçu et l'application.
				result.ProductsSkipped++
				continue
			}

			var chosen []compositionComponent
			for _, ing := range it.Ingredients {
				component, ok, err := resolver.resolve(txCtx, ing)
				if err != nil {
					return err
				}
				if !ok {
					result.IngredientsSkipped++
					continue
				}
				chosen = append(chosen, component)
			}

			toDelete, toInsert := planCompositionChanges(product.Lines, chosen, it.Mode)
			if len(toDelete) == 0 && len(toInsert) == 0 {
				result.ProductsUnchanged++
				continue
			}

			if len(toDelete) > 0 {
				placeholders, args := bulkProductPlaceholders(toDelete)
				if _, err := db.ExecContext(txCtx, fmt.Sprintf(
					`DELETE FROM requires WHERE id IN (%s)`, placeholders), args...); err != nil {
					return fmt.Errorf("composition IA : retrait de lignes: %w", err)
				}
			}
			if len(toInsert) > 0 {
				recipeID, err := r.getOrCreateRecipeID(txCtx, merchantID, product.ID)
				if err != nil {
					return err
				}
				for _, c := range toInsert {
					if err := r.insertRequireRow(txCtx, recipeID, ProductComponentUpdate{
						ComponentID: c.ID, Quantity: 0, UnitID: c.UnitID,
					}); err != nil {
						return fmt.Errorf("composition IA : ajout d'une ligne: %w", err)
					}
				}
			}
			result.ProductsUpdated++
		}

		result.ComponentsCreated = resolver.componentsCreated
		result.ComponentCategoriesCreated = resolver.categoriesCreated
		_ = r.setMenuUpdated(txCtx, merchantID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// componentResolver retrouve ou crée l'ingrédient d'un nom proposé, dans la
// transaction d'application.
type componentResolver struct {
	r          *MenuRepository
	merchantID string
	unitIDs    map[string]string

	byKey             map[string]compositionComponent
	categoryByLabel   map[string]string // FoldHeader(nom) -> merchant_categ_id
	componentsCreated int
	categoriesCreated int
}

func (r *MenuRepository) newComponentResolver(ctx context.Context, merchantID string, unitIDs map[string]string) (*componentResolver, error) {
	components, err := r.LoadCompositionComponents(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	categories, err := r.loadImportExistingComponentCategories(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	res := &componentResolver{
		r: r, merchantID: merchantID, unitIDs: unitIDs,
		byKey:           make(map[string]compositionComponent, len(components)),
		categoryByLabel: make(map[string]string, len(categories)),
	}
	for _, c := range components {
		if key := importer.IngredientKey(c.Name); key != "" {
			if _, dup := res.byKey[key]; !dup {
				res.byKey[key] = c
			}
		}
	}
	for _, c := range categories {
		label := importutil.FoldHeader(c.Name)
		if _, dup := res.categoryByLabel[label]; !dup && c.MerchantCategID != "" {
			res.categoryByLabel[label] = c.MerchantCategID
		}
	}
	return res, nil
}

// resolve rend l'ingrédient existant de même clé, ou le crée. Faux (sans
// erreur) quand il ne peut pas l'être : nom vide, ou aucune unité de mesure.
func (cr *componentResolver) resolve(ctx context.Context, ing AICompositionIngredient) (compositionComponent, bool, error) {
	cleaned := importer.CleanIngredients([]importer.AIIngredient{{Name: ing.Name, Category: ing.Category, Unit: ing.Unit}}, 1)
	if len(cleaned) == 0 {
		return compositionComponent{}, false, nil
	}
	clean := cleaned[0]
	key := importer.IngredientKey(clean.Name)
	if existing, ok := cr.byKey[key]; ok {
		return existing, true, nil
	}

	unitID, ok := cr.unitIDs[clean.Unit]
	if !ok {
		if unitID, ok = cr.unitIDs["PCE"]; !ok {
			return compositionComponent{}, false, nil
		}
	}
	categoryID, err := cr.category(ctx, clean.Category)
	if err != nil {
		return compositionComponent{}, false, err
	}

	name := clean.Name
	price := 0
	id, err := cr.r.CreateComponent(ctx, &UpdateComponentPayload{
		Name: &name, Price: &price, MerchantID: cr.merchantID, CategoryID: &categoryID, UnitID: &unitID,
	})
	if err != nil {
		return compositionComponent{}, false, fmt.Errorf("composition IA : création de l'ingrédient %q: %w", name, err)
	}
	created := compositionComponent{ID: id, Name: capitalizeFirst(strings.TrimSpace(name)), UnitID: unitID}
	cr.byKey[key] = created
	cr.componentsCreated++
	return created, true, nil
}

// category rend la catégorie d'ingrédient du code proposé (« vegetables » →
// « Légumes »), réutilisée par nom ou créée.
func (cr *componentResolver) category(ctx context.Context, code string) (string, error) {
	label := importer.IngredientCategoryLabel(code)
	if id, ok := cr.categoryByLabel[importutil.FoldHeader(label)]; ok {
		return id, nil
	}
	idStr, err := cr.r.CreateComponentCategory(ctx, &UpsertComponentCategoryPayload{Name: label, MerchantID: cr.merchantID})
	if err != nil {
		return "", fmt.Errorf("composition IA : création de la catégorie %q: %w", label, err)
	}
	categID, err := strconv.Atoi(idStr)
	if err != nil {
		return "", fmt.Errorf("composition IA : identifiant de catégorie inattendu %q: %w", idStr, err)
	}
	merchantCategID, err := cr.r.readComponentCategoryMerchantIDTx(ctx, cr.merchantID, categID)
	if err != nil {
		return "", err
	}
	cr.categoryByLabel[importutil.FoldHeader(label)] = merchantCategID
	cr.categoriesCreated++
	return merchantCategID, nil
}
