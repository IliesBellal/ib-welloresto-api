package order_life_cycle

import (
	"context"
	"database/sql"
	"strings"

	"welloresto-api/internal/costing"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

// selectedOptionsOf flattens a product's configuration payload into the
// option id/quantity pairs resolveOrderItemCost needs — mirroring exactly
// what insertExtrasWithoutsConfigs/UpdateOrder write to
// order_item_configuration (every option in the payload, unfiltered by
// Selected), so the frozen cost never diverges from what's actually stored
// for this line's configuration.
func selectedOptionsOf(cfg *models.ProductConfiguration) []costOptionSelection {
	if cfg == nil {
		return nil
	}
	var out []costOptionSelection
	for _, attr := range cfg.Attributes {
		for _, opt := range attr.Options {
			out = append(out, costOptionSelection{OptionID: opt.ID, Quantity: opt.Quantity})
		}
	}
	return out
}

// costOptionSelection is the minimal shape resolveOrderItemCost needs from a
// selected configurable option: its id (configurable_attribute_options.id)
// and how many were selected on this line (order_item_configuration.quantity,
// per unit of the product — same convention as extra_price, which the
// existing revenue-side queries already multiply by orderitems.quantity).
type costOptionSelection struct {
	OptionID string
	Quantity int
}

// resolveOrderItemCost snapshots the frozen unit cost-of-goods (B2) for one
// order line: the product's recipe cost plus the ingredient cost of any
// selected configurable options that carry a linked component
// (configurable_attribute_options.component_id — migration 079's "L'onglet
// Options a besoin de la même chose" resolution, reused here rather than
// duplicated). Returns (nil, reason) when the cost can't be trusted — never a
// guessed 0. A query error is treated the same as "can't resolve": logged and
// reported as INCOMPLETE_RECIPE rather than failing the order write, since
// cost enrichment must never block a sale.
func (r *OrdersLifeCycleRepository) resolveOrderItemCost(ctx context.Context, merchantID, productID string, options []costOptionSelection) (costCents *int, reason *string) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var recipeID int64
	err := db.QueryRowContext(ctx,
		`SELECT recipe_id FROM recipes WHERE product_id = ? AND merchant_id = ?`,
		productID, merchantID,
	).Scan(&recipeID)
	if err == sql.ErrNoRows {
		return nil, helpers.StringPtr(costing.ReasonNoRecipe)
	}
	if err != nil {
		log.Warn("resolveOrderItemCost: recipe lookup failed", zap.String("product_id", productID), zap.Error(err))
		return nil, helpers.StringPtr(costing.ReasonIncompleteRecipe)
	}

	baseCost, ok := r.resolveRecipeCost(ctx, merchantID, recipeID)
	if !ok {
		return nil, helpers.StringPtr(costing.ReasonIncompleteRecipe)
	}

	optionsCost, ok := r.resolveOptionsCost(ctx, merchantID, options)
	if !ok {
		return nil, helpers.StringPtr(costing.ReasonIncompleteRecipe)
	}

	total := costing.RoundToCents(baseCost + optionsCost)
	return &total, nil
}

// resolveRecipeCost sums the cost of every enabled ingredient a recipe
// requires. ok is false when the recipe can't be fully priced: an empty
// recipe (rows exist in `recipes` but none in `requires`), a required
// consumable_id (this lot only resolves components.purchase_price, per the
// brief — a consumable-backed requirement is treated as unresolvable rather
// than silently excluded from the sum), a missing/zero purchase price, or a
// unit conversion with no matching unit_of_measure_convert row.
func (r *OrdersLifeCycleRepository) resolveRecipeCost(ctx context.Context, merchantID string, recipeID int64) (costCents float64, ok bool) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
		SELECT rq.component_id, rq.consumable_id, rq.quantity, rq.unit_of_measure,
		       c.purchase_price, c.purchase_price_quantity, c.unit_of_measure,
		       conv.ratio
		FROM requires rq
		LEFT JOIN components c ON c.component_id = rq.component_id AND c.merchant_id = ?
		LEFT JOIN unit_of_measure_convert conv ON conv.id_from = rq.unit_of_measure AND conv.id_to = c.unit_of_measure
		WHERE rq.recipe_id = ? AND rq.enabled = true
	`, merchantID, recipeID)
	if err != nil {
		logger.FromContext(ctx).Warn("resolveRecipeCost: requires query failed", zap.Int64("recipe_id", recipeID), zap.Error(err))
		return 0, false
	}
	defer rows.Close()

	var total float64
	var rowCount int
	for rows.Next() {
		rowCount++
		var componentID, consumableID sql.NullInt64
		var requiredQty float64
		var requiredUOM int
		var purchasePrice sql.NullInt64
		var purchasePriceQty sql.NullFloat64
		var componentUOM sql.NullInt64
		var ratio sql.NullFloat64

		if err := rows.Scan(&componentID, &consumableID, &requiredQty, &requiredUOM,
			&purchasePrice, &purchasePriceQty, &componentUOM, &ratio); err != nil {
			logger.FromContext(ctx).Warn("resolveRecipeCost: scan failed", zap.Int64("recipe_id", recipeID), zap.Error(err))
			return 0, false
		}

		if !componentID.Valid || !purchasePrice.Valid || !purchasePriceQty.Valid || !componentUOM.Valid {
			// consumable_id-backed requirement, or the joined component row
			// is missing — not resolvable by this lot's logic.
			return 0, false
		}

		var ratioPtr *float64
		if ratio.Valid {
			ratioPtr = &ratio.Float64
		}

		cost, ok := costing.UnitCost(requiredQty, requiredUOM, int(componentUOM.Int64), ratioPtr, int(purchasePrice.Int64), purchasePriceQty.Float64)
		if !ok {
			return 0, false
		}
		total += cost
	}
	if err := rows.Err(); err != nil {
		logger.FromContext(ctx).Warn("resolveRecipeCost: rows iteration failed", zap.Int64("recipe_id", recipeID), zap.Error(err))
		return 0, false
	}
	if rowCount == 0 {
		// Recipe shell with no ingredients — a configuration defect, not "no recipe".
		return 0, false
	}

	return total, true
}

// recipeCostEntry is one product's batch-resolved base recipe cost.
// hasRecipe distinguishes "no recipe row at all" (absent from the map
// entirely, see resolveRecipeCostsBatch) from "recipe exists but ok=false"
// (empty recipe shell, or an ingredient that can't be priced/converted) —
// the NO_RECIPE vs INCOMPLETE_RECIPE distinction the migration's
// cost_price_reason column exists for.
type recipeCostEntry struct {
	costCents float64
	ok        bool
}

// optionCostEntry is one configurable_attribute_options row's resolved cost
// per single selection (i.e. before multiplying by how many were selected on
// a given line). ok=true with costCents=0 means "no ingredient linked to
// this option" (a real, deterministic zero); ok=false means a linked
// ingredient exists but can't be priced/converted.
type optionCostEntry struct {
	costCents float64
	ok        bool
}

// lineCostResult mirrors what resolveOrderItemCost returns, but as plain
// values (not *int/*string) so resolveOrderItemCostsForOrder can build a
// slice of them before handing each line off to the caller.
type lineCostResult struct {
	costPriceUnit   *int
	costPriceReason *string
}

// resolveOrderItemCostsForOrder is the CreateOrder-path counterpart of
// resolveOrderItemCost: it resolves every line of a whole order's worth of
// products in ~2-3 SQL round trips total, instead of ~2 round trips PER
// LINE. Measured against staging (docs/decisions.md): each line item costs
// two indexed-but-network-round-trip-bound queries (recipe lookup, requires/
// components join), so an unbatched N-line order roughly doubles
// CreateOrder's total DB round trips. Batching collapses that back to a
// constant few queries regardless of order size. UpdateOrder does NOT use
// this — it edits far fewer lines per call (typically one), so the simpler
// per-item resolveOrderItemCost is used there; see docs/decisions.md for why
// that asymmetry is a deliberate, scoped choice rather than an oversight.
//
// The returned slice is aligned index-for-index with products (same product
// appearing on two lines with different options resolves independently).
func (r *OrdersLifeCycleRepository) resolveOrderItemCostsForOrder(ctx context.Context, merchantID string, products []models.OrderProductPayload) []lineCostResult {
	results := make([]lineCostResult, len(products))

	productIDs := make([]string, 0, len(products))
	var allOptionIDs []string
	for _, p := range products {
		productIDs = append(productIDs, p.ProductID)
		for _, sel := range selectedOptionsOf(p.Config) {
			allOptionIDs = append(allOptionIDs, sel.OptionID)
		}
	}

	recipeCosts := r.resolveRecipeCostsBatch(ctx, merchantID, productIDs)
	optionCosts := r.resolveOptionCostsBatch(ctx, merchantID, allOptionIDs)

	for i, p := range products {
		entry, hasRecipe := recipeCosts[p.ProductID]
		if !hasRecipe {
			results[i] = lineCostResult{costPriceReason: helpers.StringPtr(costing.ReasonNoRecipe)}
			continue
		}
		if !entry.ok {
			results[i] = lineCostResult{costPriceReason: helpers.StringPtr(costing.ReasonIncompleteRecipe)}
			continue
		}

		total := entry.costCents
		resolvable := true
		for _, sel := range selectedOptionsOf(p.Config) {
			opt, found := optionCosts[sel.OptionID]
			if !found || !opt.ok {
				resolvable = false
				break
			}
			total += opt.costCents * float64(sel.Quantity)
		}
		if !resolvable {
			results[i] = lineCostResult{costPriceReason: helpers.StringPtr(costing.ReasonIncompleteRecipe)}
			continue
		}

		cost := costing.RoundToCents(total)
		results[i] = lineCostResult{costPriceUnit: &cost}
	}

	return results
}

// resolveRecipeCostsBatch resolves the base recipe cost for every product in
// productIDs at once: one query to find which have a recipe at all, one
// query for every one of those recipes' requires/components/conversion rows
// together. A product absent from the returned map has no recipe row
// (NO_RECIPE); a product present with ok=false has a recipe that can't be
// fully priced (INCOMPLETE_RECIPE) — same rules as resolveRecipeCost, batched.
func (r *OrdersLifeCycleRepository) resolveRecipeCostsBatch(ctx context.Context, merchantID string, productIDs []string) map[string]recipeCostEntry {
	results := make(map[string]recipeCostEntry)
	uniqueProductIDs := dedupeStrings(productIDs)
	if len(uniqueProductIDs) == 0 {
		return results
	}

	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	placeholders := make([]string, len(uniqueProductIDs))
	args := make([]interface{}, 0, len(uniqueProductIDs)+1)
	args = append(args, merchantID)
	for i, id := range uniqueProductIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	recipeIDToProductID := make(map[int64]string)
	rows, err := db.QueryContext(ctx, `
		SELECT product_id, recipe_id FROM recipes
		WHERE merchant_id = ? AND product_id IN (`+strings.Join(placeholders, ",")+`)
	`, args...)
	if err != nil {
		log.Warn("resolveRecipeCostsBatch: recipe lookup failed", zap.Error(err))
		// Can't tell "no recipe" from "couldn't check" — report every
		// requested product as incomplete rather than silently NO_RECIPE.
		for _, id := range uniqueProductIDs {
			results[id] = recipeCostEntry{ok: false}
		}
		return results
	}
	func() {
		defer rows.Close()
		for rows.Next() {
			var productID string
			var recipeID int64
			if err := rows.Scan(&productID, &recipeID); err != nil {
				log.Warn("resolveRecipeCostsBatch: scan failed", zap.Error(err))
				continue
			}
			recipeIDToProductID[recipeID] = productID
		}
	}()
	if len(recipeIDToProductID) == 0 {
		return results // every product genuinely has no recipe row.
	}

	recipeIDs := make([]int64, 0, len(recipeIDToProductID))
	for rid := range recipeIDToProductID {
		recipeIDs = append(recipeIDs, rid)
	}
	recipePlaceholders := make([]string, len(recipeIDs))
	recipeArgs := make([]interface{}, 0, len(recipeIDs)+1)
	recipeArgs = append(recipeArgs, merchantID)
	for i, rid := range recipeIDs {
		recipePlaceholders[i] = "?"
		recipeArgs = append(recipeArgs, rid)
	}

	rows, err = db.QueryContext(ctx, `
		SELECT rq.recipe_id, rq.component_id, rq.consumable_id, rq.quantity, rq.unit_of_measure,
		       c.purchase_price, c.purchase_price_quantity, c.unit_of_measure,
		       conv.ratio
		FROM requires rq
		LEFT JOIN components c ON c.component_id = rq.component_id AND c.merchant_id = ?
		LEFT JOIN unit_of_measure_convert conv ON conv.id_from = rq.unit_of_measure AND conv.id_to = c.unit_of_measure
		WHERE rq.recipe_id IN (`+strings.Join(recipePlaceholders, ",")+`) AND rq.enabled = true
	`, recipeArgs...)
	if err != nil {
		log.Warn("resolveRecipeCostsBatch: requires query failed", zap.Error(err))
		for rid, productID := range recipeIDToProductID {
			_ = rid
			results[productID] = recipeCostEntry{ok: false}
		}
		return results
	}
	defer rows.Close()

	totals := make(map[int64]float64)
	unresolvable := make(map[int64]bool)
	rowCounts := make(map[int64]int)
	for rows.Next() {
		var recipeID int64
		var componentID, consumableID sql.NullInt64
		var requiredQty float64
		var requiredUOM int
		var purchasePrice sql.NullInt64
		var purchasePriceQty sql.NullFloat64
		var componentUOM sql.NullInt64
		var ratio sql.NullFloat64

		if err := rows.Scan(&recipeID, &componentID, &consumableID, &requiredQty, &requiredUOM,
			&purchasePrice, &purchasePriceQty, &componentUOM, &ratio); err != nil {
			log.Warn("resolveRecipeCostsBatch: requires scan failed", zap.Error(err))
			continue
		}
		rowCounts[recipeID]++

		if unresolvable[recipeID] {
			continue
		}
		if !componentID.Valid || !purchasePrice.Valid || !purchasePriceQty.Valid || !componentUOM.Valid {
			unresolvable[recipeID] = true
			continue
		}

		var ratioPtr *float64
		if ratio.Valid {
			ratioPtr = &ratio.Float64
		}
		cost, ok := costing.UnitCost(requiredQty, requiredUOM, int(componentUOM.Int64), ratioPtr, int(purchasePrice.Int64), purchasePriceQty.Float64)
		if !ok {
			unresolvable[recipeID] = true
			continue
		}
		totals[recipeID] += cost
	}
	if err := rows.Err(); err != nil {
		log.Warn("resolveRecipeCostsBatch: requires rows iteration failed", zap.Error(err))
	}

	for recipeID, productID := range recipeIDToProductID {
		if unresolvable[recipeID] || rowCounts[recipeID] == 0 {
			results[productID] = recipeCostEntry{ok: false}
			continue
		}
		results[productID] = recipeCostEntry{costCents: totals[recipeID], ok: true}
	}
	return results
}

// resolveOptionCostsBatch resolves, for every configurable_attribute_options
// id in optionIDs, the cost of a single selection of that option — the
// per-line total is selectedQuantity * this value, computed by the caller
// (each line can select the same option a different number of times).
func (r *OrdersLifeCycleRepository) resolveOptionCostsBatch(ctx context.Context, merchantID string, optionIDs []string) map[string]optionCostEntry {
	results := make(map[string]optionCostEntry)
	uniqueOptionIDs := dedupeStrings(optionIDs)
	if len(uniqueOptionIDs) == 0 {
		return results
	}

	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	placeholders := make([]string, len(uniqueOptionIDs))
	args := make([]interface{}, 0, len(uniqueOptionIDs)+1)
	args = append(args, merchantID)
	for i, id := range uniqueOptionIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT cao.id, cao.component_id, cao.quantity, cao.unit_of_measure,
		       c.purchase_price, c.purchase_price_quantity, c.unit_of_measure,
		       conv.ratio
		FROM configurable_attribute_options cao
		LEFT JOIN components c ON c.component_id = cao.component_id AND c.merchant_id = ?
		LEFT JOIN unit_of_measure_convert conv ON conv.id_from = cao.unit_of_measure AND conv.id_to = c.unit_of_measure
		WHERE cao.id IN (`+strings.Join(placeholders, ",")+`)
	`, args...)
	if err != nil {
		log.Warn("resolveOptionCostsBatch: query failed", zap.Error(err))
		for _, id := range uniqueOptionIDs {
			results[id] = optionCostEntry{ok: false}
		}
		return results
	}
	defer rows.Close()

	for rows.Next() {
		var optionID string
		var componentID sql.NullInt64
		var optQty sql.NullFloat64
		var optUOM sql.NullInt64
		var purchasePrice sql.NullInt64
		var purchasePriceQty sql.NullFloat64
		var componentUOM sql.NullInt64
		var ratio sql.NullFloat64

		if err := rows.Scan(&optionID, &componentID, &optQty, &optUOM,
			&purchasePrice, &purchasePriceQty, &componentUOM, &ratio); err != nil {
			log.Warn("resolveOptionCostsBatch: scan failed", zap.Error(err))
			continue
		}

		if !componentID.Valid {
			results[optionID] = optionCostEntry{ok: true} // no ingredient linked: a real 0.
			continue
		}
		if !optQty.Valid || !optUOM.Valid || !purchasePrice.Valid || !purchasePriceQty.Valid || !componentUOM.Valid {
			results[optionID] = optionCostEntry{ok: false}
			continue
		}
		var ratioPtr *float64
		if ratio.Valid {
			ratioPtr = &ratio.Float64
		}
		cost, ok := costing.UnitCost(optQty.Float64, int(optUOM.Int64), int(componentUOM.Int64), ratioPtr, int(purchasePrice.Int64), purchasePriceQty.Float64)
		results[optionID] = optionCostEntry{costCents: cost, ok: ok}
	}
	if err := rows.Err(); err != nil {
		log.Warn("resolveOptionCostsBatch: rows iteration failed", zap.Error(err))
	}

	return results
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// resolveOptionsCost sums the ingredient cost of the selected configurable
// options that carry a linked component. An option with no linked component
// contributes 0 deliberately (a known, deterministic fact — "Sans glaçons"
// truly costs nothing), never NULL. ok is false only when a *linked* option's
// cost can't be resolved (missing price, missing conversion) — that is a
// configuration defect, same bucket as an incomplete recipe.
func (r *OrdersLifeCycleRepository) resolveOptionsCost(ctx context.Context, merchantID string, options []costOptionSelection) (costCents float64, ok bool) {
	if len(options) == 0 {
		return 0, true
	}
	db := dbx.GetDB(ctx, r.database)

	placeholders := make([]string, len(options))
	args := make([]interface{}, 0, len(options)+1)
	args = append(args, merchantID)
	qtyByOption := make(map[string]int, len(options))
	for i, o := range options {
		placeholders[i] = "?"
		args = append(args, o.OptionID)
		qtyByOption[o.OptionID] += o.Quantity
	}

	rows, err := db.QueryContext(ctx, `
		SELECT cao.id, cao.component_id, cao.quantity, cao.unit_of_measure,
		       c.purchase_price, c.purchase_price_quantity, c.unit_of_measure,
		       conv.ratio
		FROM configurable_attribute_options cao
		LEFT JOIN components c ON c.component_id = cao.component_id AND c.merchant_id = ?
		LEFT JOIN unit_of_measure_convert conv ON conv.id_from = cao.unit_of_measure AND conv.id_to = c.unit_of_measure
		WHERE cao.id IN (`+strings.Join(placeholders, ",")+`)
	`, args...)
	if err != nil {
		logger.FromContext(ctx).Warn("resolveOptionsCost: query failed", zap.Error(err))
		return 0, false
	}
	defer rows.Close()

	var total float64
	for rows.Next() {
		var optionID string
		var componentID sql.NullInt64
		var optQty sql.NullFloat64
		var optUOM sql.NullInt64
		var purchasePrice sql.NullInt64
		var purchasePriceQty sql.NullFloat64
		var componentUOM sql.NullInt64
		var ratio sql.NullFloat64

		if err := rows.Scan(&optionID, &componentID, &optQty, &optUOM,
			&purchasePrice, &purchasePriceQty, &componentUOM, &ratio); err != nil {
			logger.FromContext(ctx).Warn("resolveOptionsCost: scan failed", zap.Error(err))
			return 0, false
		}

		if !componentID.Valid {
			// No ingredient linked to this option: a real, known 0 — not missing data.
			continue
		}
		if !optQty.Valid || !optUOM.Valid || !purchasePrice.Valid || !purchasePriceQty.Valid || !componentUOM.Valid {
			return 0, false
		}

		var ratioPtr *float64
		if ratio.Valid {
			ratioPtr = &ratio.Float64
		}

		unitCost, ok := costing.UnitCost(optQty.Float64, int(optUOM.Int64), int(componentUOM.Int64), ratioPtr, int(purchasePrice.Int64), purchasePriceQty.Float64)
		if !ok {
			return 0, false
		}

		selectedQty := qtyByOption[optionID]
		total += unitCost * float64(selectedQty)
	}
	if err := rows.Err(); err != nil {
		logger.FromContext(ctx).Warn("resolveOptionsCost: rows iteration failed", zap.Error(err))
		return 0, false
	}

	return total, true
}
