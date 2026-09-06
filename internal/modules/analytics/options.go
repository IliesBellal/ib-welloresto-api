package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
)

// ---- Options (POST /analytics/options) ----
//
// PROMPT 17, template-identical to Produits (PROMPT 16, products.go) for cost/
// margin/pagination handling — see that file's doc comment for the rules
// reused here verbatim (partial-aggregation guard, NULL-never-0, server-side
// pagination/sort).
//
// This tab covers two structurally different write paths under one table:
//   - configurable options (order_item_configuration + configurable_attribute_
//     options), split into "paid" (extra_price > 0) and "free" (extra_price =
//     0) — a free option still generates volume, never CA;
//   - ingredient removals (the `without` table), always "removed" — no price
//     was ever charged for removing an ingredient, so revenue is a real,
//     deterministic 0, and no cost snapshot exists for this table at all
//     (unlike order_item_configuration.cost_price_unit), so cost is always
//     NULL/not-applicable for these rows, never a 0.
//
// Both are read through ONE combined CTE (UNION ALL'd, read exactly once —
// see GetOptionsPage's doc comment) rather than two separate queries, so the
// tab's KPI tiles and paginated table share a single definition of "what
// counts", the same way Produits' htLineJoins is the one definition every
// query in that file builds from.
//
// quantity_sold (CA-driving) and the adoption numerator are DIFFERENT numbers
// for a configurable option: order_item_configuration.quantity lets the same
// option be selected more than once on a single line (e.g. "2x extra sauce"),
// so quantity_sold (= oic.quantity * oi.quantity, matching configurable_
// attribute_options.extra_price's own per-selection convention) can exceed
// the product's own unit sales. Adoption asks a different question — "what
// share of this product's units picked this option at least once" — so its
// numerator is oi.quantity alone (one product unit counted once regardless of
// how many times the option was picked on that line), never multiplied by
// oic.quantity. Removed-ingredient rows have no such distinction (`without`
// carries no quantity of its own): quantity_sold and the adoption numerator
// are the same value there.

// optionTypesFilter validates the caller's requested option_types against the
// 3 known values, defaulting to all 3 when empty — mirrors the frontend's
// MultiFilter default (all three checked). An unknown value is a 400, not a
// silent drop: PROMPT 17 §3 flags the mock's optionTypes filter as passed but
// never actually applied by the service — the fix must reject nonsense, not
// just accept and ignore it a second way.
func optionTypesFilter(requested []string) ([]string, bool) {
	if len(requested) == 0 {
		return []string{OptionTypePaid, OptionTypeFree, OptionTypeRemoved}, true
	}
	seen := make(map[string]bool, len(requested))
	for _, t := range requested {
		switch t {
		case OptionTypePaid, OptionTypeFree, OptionTypeRemoved:
			seen[t] = true
		default:
			return nil, false
		}
	}
	out := make([]string, 0, len(seen))
	for _, t := range []string{OptionTypePaid, OptionTypeFree, OptionTypeRemoved} {
		if seen[t] {
			out = append(out, t)
		}
	}
	return out, true
}

// optionsCombinedCTE is the shared body of GetOptionsScopeTotals and
// GetOptionsPage: one UNION ALL of the two source tables described in this
// file's doc comment, each row already carrying everything the outer query
// needs (units for CA, adoption_units for the adoption/cost denominator,
// extra_price, and the frozen cost snapshot). Read exactly once per call —
// GetOptionsPage's COUNT(*) OVER() runs in the same aggregating pass over
// this CTE, never a second scan just to count pages (PROMPT 17 §4's
// performance requirement, same discipline as products.go's GetProductsPage).
//
// scoped_orders/scoped_items/scoped_configs/scoped_withouts stage the scope
// filter BEFORE either source table is touched, materializing scoped_configs/
// scoped_withouts explicitly (Postgres 12+ MATERIALIZED, not left to the
// planner's default heuristic): without this staging, the planner drives the
// join from configurable_attribute_options — EVERY merchant's option catalog,
// not just this request's — probing order_item_configuration by option id
// per row, which costs more as the system-wide option catalog grows,
// regardless of this one merchant's own order volume. Measured against
// staging (merchant 212, 12 months, 2026-09-05): 2041 ms unstaged vs 438 ms
// staged, same result set, no disk spill on either plan (PROMPT 17 §4).
// order_item_configuration/without are themselves small tables system-wide
// (~35k / ~3.1k rows on staging) — cheap to scan in full once narrowed to
// this merchant's own order_item_ids via a hash join, which is exactly what
// materializing scoped_configs/scoped_withouts forces to happen first.
//
// args order: scope args (merchant_ids, start, end — bound once, in
// scoped_orders), then option_types array twice (once per UNION branch's own
// filter — each `?` is its own positional parameter, see dbx.Rebind, so the
// same slice value must be passed twice even though it reads identically).
const optionsCombinedCTE = `
	scoped_orders AS (
		SELECT o.order_id FROM orders o WHERE %s
	),
	scoped_items AS (
		SELECT oi.order_item_id, oi.product_id, oi.quantity
		FROM orderitems oi
		INNER JOIN scoped_orders so ON so.order_id = oi.order_id
	),
	scoped_configs AS MATERIALIZED (
		SELECT oic.order_item_id, oic.configuration_attribute_option_id,
			oic.quantity AS oic_quantity, oic.cost_price_unit, oic.cost_price_reason,
			si.product_id, si.quantity AS item_quantity
		FROM scoped_items si
		INNER JOIN order_item_configuration oic ON oic.order_item_id = si.order_item_id
	),
	scoped_withouts AS MATERIALIZED (
		SELECT w.component_id, si.product_id, si.quantity AS item_quantity
		FROM scoped_items si
		INNER JOIN without w ON w.order_item_id = si.order_item_id AND w.order_item_id > 0
	),
	combined AS (
		SELECT
			cao.id::text AS entity_id,
			cao.title AS name,
			cat.title AS attribute_name,
			p.product_id::text AS product_id,
			p.name AS product_name,
			CASE WHEN cao.extra_price > 0 THEN 'paid' ELSE 'free' END AS option_type,
			sc.oic_quantity * sc.item_quantity AS units,
			sc.item_quantity AS adoption_units,
			cao.extra_price AS extra_price,
			sc.cost_price_unit AS cost_price_unit,
			sc.cost_price_reason AS cost_price_reason
		FROM scoped_configs sc
		INNER JOIN configurable_attribute_options cao ON cao.id = sc.configuration_attribute_option_id
		INNER JOIN products p ON p.product_id = sc.product_id
		LEFT JOIN configurable_attributes cat ON cat.id = cao.configurable_attribute_id
		WHERE (CASE WHEN cao.extra_price > 0 THEN 'paid' ELSE 'free' END) = ANY(?)

		UNION ALL

		SELECT
			c.component_id::text AS entity_id,
			c.name AS name,
			NULL AS attribute_name,
			p.product_id::text AS product_id,
			p.name AS product_name,
			'removed' AS option_type,
			sw.item_quantity AS units,
			sw.item_quantity AS adoption_units,
			0 AS extra_price,
			NULL::integer AS cost_price_unit,
			NULL::varchar AS cost_price_reason
		FROM scoped_withouts sw
		INNER JOIN products p ON p.product_id = sw.product_id
		INNER JOIN components c ON c.component_id = sw.component_id
		WHERE 'removed' = ANY(?)
	)
`

// OptionsScopeTotals is GetOptionsScopeTotals' raw row — see
// ProductsScopeTotals (products.go) for why this is a second, cheap ungrouped
// pass rather than derived from the paginated GetOptionsPage.
type OptionsScopeTotals struct {
	QuantitySold             int64
	RevenueTTCCents          int64
	CostKnownRevenueTTCCents int64
	CostPriceCents           int64
	NoRecipeQuantity         int64
	IncompleteRecipeQuantity int64
}

// GetOptionsScopeTotals sums every row matching the requested optionTypes,
// across the whole scope (not just the current page) — the tab's KPI tiles
// and ProductsCostCoverage-style aggregate margin.
func (r *Repository) GetOptionsScopeTotals(ctx context.Context, merchantIDs []string, optionTypes []string, startUTC, endUTC time.Time) (OptionsScopeTotals, error) {
	where, scopeArgs := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)

	query := "WITH " + fmt.Sprintf(optionsCombinedCTE, where) + `
		SELECT
			COALESCE(SUM(units), 0),
			COALESCE(SUM(units * extra_price), 0),
			COALESCE(SUM(units * extra_price) FILTER (WHERE cost_price_unit IS NOT NULL), 0),
			COALESCE(SUM(cost_price_unit * adoption_units) FILTER (WHERE cost_price_unit IS NOT NULL), 0),
			COALESCE(SUM(adoption_units) FILTER (WHERE cost_price_unit IS NULL AND cost_price_reason = 'NO_RECIPE'), 0),
			COALESCE(SUM(adoption_units) FILTER (WHERE cost_price_unit IS NULL AND cost_price_reason = 'INCOMPLETE_RECIPE'), 0)
		FROM combined
	`

	args := append([]interface{}{}, scopeArgs...)
	args = append(args, optionTypes, optionTypes)

	var totals OptionsScopeTotals
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, args...).Scan(
			&totals.QuantitySold, &totals.RevenueTTCCents,
			&totals.CostKnownRevenueTTCCents, &totals.CostPriceCents,
			&totals.NoRecipeQuantity, &totals.IncompleteRecipeQuantity,
		)
	})
	if err != nil {
		return OptionsScopeTotals{}, fmt.Errorf("get options scope totals: %w", err)
	}
	return totals, nil
}

// OptionAggRow is GetOptionsPage's raw row — see ProductAggRow (products.go)
// for the nullable-cost convention it mirrors.
type OptionAggRow struct {
	EntityID      string
	Name          string
	AttributeName sql.NullString
	ProductID     string
	ProductName   string
	OptionType    string

	QuantitySold             int64
	AdoptionUnits            int64
	RevenueTTCCents          int64
	CostKnownQuantity        int64
	CostKnownRevenueTTCCents int64
	CostPriceCents           sql.NullInt64
	NoRecipeQuantity         int64
	IncompleteRecipeQuantity int64
}

// optionsSortColumn whitelists OptionsRequest.SortBy — never string-
// interpolating the client's own value into ORDER BY, same discipline as
// productsSortColumn.
func optionsSortColumn(sortBy string) string {
	switch sortBy {
	case OptionsSortRevenue:
		return "revenue_ttc_cents"
	case OptionsSortMargin:
		return "margin_cents"
	default:
		return "quantity_sold"
	}
}

// GetOptionsPage returns one page of per-(option-or-removed-ingredient,
// product) aggregates, sorted and counted server-side — see this file's doc
// comment on optionsCombinedCTE for why the whole thing is one pass.
func (r *Repository) GetOptionsPage(ctx context.Context, merchantIDs []string, optionTypes []string, sortBy, sortDir string, page, pageSize int, startUTC, endUTC time.Time) ([]OptionAggRow, int64, error) {
	where, scopeArgs := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)

	sortColumn := optionsSortColumn(sortBy)
	dir := "DESC"
	if strings.EqualFold(sortDir, "asc") {
		dir = "ASC"
	}
	nullsClause := ""
	if sortColumn == "margin_cents" {
		nullsClause = " NULLS LAST"
	}

	query := "WITH " + fmt.Sprintf(optionsCombinedCTE, where) + `
		SELECT
			entity_id, name, attribute_name, product_id, product_name, option_type,
			quantity_sold, adoption_units, revenue_ttc_cents,
			cost_known_quantity, cost_known_revenue_ttc_cents, cost_price_cents,
			CASE WHEN cost_price_cents IS NOT NULL THEN cost_known_revenue_ttc_cents - cost_price_cents ELSE NULL END AS margin_cents,
			no_recipe_quantity, incomplete_recipe_quantity,
			COUNT(*) OVER() AS total_rows
		FROM (
			SELECT
				entity_id, name, attribute_name, product_id, product_name, option_type,
				SUM(units) AS quantity_sold,
				SUM(adoption_units) AS adoption_units,
				SUM(units * extra_price) AS revenue_ttc_cents,
				COALESCE(SUM(adoption_units) FILTER (WHERE cost_price_unit IS NOT NULL), 0) AS cost_known_quantity,
				COALESCE(SUM(units * extra_price) FILTER (WHERE cost_price_unit IS NOT NULL), 0) AS cost_known_revenue_ttc_cents,
				SUM(cost_price_unit * adoption_units) FILTER (WHERE cost_price_unit IS NOT NULL) AS cost_price_cents,
				COALESCE(SUM(adoption_units) FILTER (WHERE cost_price_unit IS NULL AND cost_price_reason = 'NO_RECIPE'), 0) AS no_recipe_quantity,
				COALESCE(SUM(adoption_units) FILTER (WHERE cost_price_unit IS NULL AND cost_price_reason = 'INCOMPLETE_RECIPE'), 0) AS incomplete_recipe_quantity
			FROM combined
			GROUP BY entity_id, name, attribute_name, product_id, product_name, option_type
		) agg
		ORDER BY ` + sortColumn + ` ` + dir + nullsClause + `, entity_id ASC, product_id ASC
		LIMIT ? OFFSET ?
	`

	args := append([]interface{}{}, scopeArgs...)
	args = append(args, optionTypes, optionTypes)
	args = append(args, pageSize, (page-1)*pageSize)

	var result []OptionAggRow
	var totalRows int64
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row OptionAggRow
			var marginCents sql.NullInt64
			if err := rows.Scan(
				&row.EntityID, &row.Name, &row.AttributeName, &row.ProductID, &row.ProductName, &row.OptionType,
				&row.QuantitySold, &row.AdoptionUnits, &row.RevenueTTCCents,
				&row.CostKnownQuantity, &row.CostKnownRevenueTTCCents, &row.CostPriceCents,
				&marginCents,
				&row.NoRecipeQuantity, &row.IncompleteRecipeQuantity,
				&totalRows,
			); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, fmt.Errorf("get options page: %w", err)
	}
	return result, totalRows, nil
}

// GetOptionsProductTotals returns each product's own total unit sales in
// scope (SUM(oi.quantity), no options join) — the adoption rate's
// denominator, bounded to exactly the current page's product_ids (never the
// full catalog), same pattern as GetProductsPreviousRevenue.
func (r *Repository) GetOptionsProductTotals(ctx context.Context, merchantIDs []string, productIDs []string, startUTC, endUTC time.Time) (map[string]int64, error) {
	result := make(map[string]int64, len(productIDs))
	if len(productIDs) == 0 {
		return result, nil
	}

	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT p.product_id::text, SUM(oi.quantity)
		FROM orderitems oi
		INNER JOIN orders o ON o.order_id = oi.order_id
		INNER JOIN products p ON p.product_id = oi.product_id
	`) + "\nWHERE " + where + `
			AND p.product_id::text = ANY(?)
		GROUP BY p.product_id
	`
	args = append(args, productIDs)

	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var productID string
			var qty int64
			if err := rows.Scan(&productID, &qty); err != nil {
				return err
			}
			result[productID] = qty
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get options product totals: %w", err)
	}
	return result, nil
}

// OptionBasketShare is one entity's contribution to the basket-impact
// comparison: how many distinct scope orders contained it at least once, and
// the sum of those orders' own TTC price (orders.price, counted once per
// order regardless of how many lines/quantity carried the entity) — see
// basketImpactCents (service.go) for how this turns into a signed delta
// against every other scope order.
type OptionBasketShare struct {
	OrderCount    int64
	OrderPriceSum int64
}

// GetOptionsBasketShares returns, for each requested option id, the distinct-
// order count/price-sum needed for the basket-impact comparison — bounded to
// the current page's option ids, same "at most a page" discipline as
// GetOptionsProductTotals. Removed-ingredient entities use
// GetOptionsBasketSharesRemoved below (different join, `without` has no
// extra_price/option row to key off).
func (r *Repository) GetOptionsBasketShares(ctx context.Context, merchantIDs []string, optionIDs []string, startUTC, endUTC time.Time) (map[string]OptionBasketShare, error) {
	result := make(map[string]OptionBasketShare, len(optionIDs))
	if len(optionIDs) == 0 {
		return result, nil
	}
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT entity_id, COUNT(*), COALESCE(SUM(order_price), 0)
		FROM (
			SELECT DISTINCT cao.id::text AS entity_id, o.order_id, o.price AS order_price
			FROM order_item_configuration oic
			INNER JOIN orderitems oi ON oi.order_item_id = oic.order_item_id
			INNER JOIN orders o ON o.order_id = oi.order_id
			INNER JOIN configurable_attribute_options cao ON cao.id = oic.configuration_attribute_option_id
	`) + "\t\t\tWHERE " + where + ` AND cao.id::text = ANY(?)
		) d
		GROUP BY entity_id
	`
	args = append(args, optionIDs)
	return r.scanBasketShares(ctx, query, args)
}

// GetOptionsBasketSharesRemoved mirrors GetOptionsBasketShares for
// removed-ingredient rows, joined through `without` instead of
// order_item_configuration.
func (r *Repository) GetOptionsBasketSharesRemoved(ctx context.Context, merchantIDs []string, componentIDs []string, startUTC, endUTC time.Time) (map[string]OptionBasketShare, error) {
	result := make(map[string]OptionBasketShare, len(componentIDs))
	if len(componentIDs) == 0 {
		return result, nil
	}
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT entity_id, COUNT(*), COALESCE(SUM(order_price), 0)
		FROM (
			SELECT DISTINCT w.component_id::text AS entity_id, o.order_id, o.price AS order_price
			FROM without w
			INNER JOIN orderitems oi ON oi.order_item_id = w.order_item_id
			INNER JOIN orders o ON o.order_id = oi.order_id
	`) + "\t\t\tWHERE w.order_item_id > 0 AND " + where + ` AND w.component_id::text = ANY(?)
		) d
		GROUP BY entity_id
	`
	args = append(args, componentIDs)
	return r.scanBasketShares(ctx, query, args)
}

func (r *Repository) scanBasketShares(ctx context.Context, query string, args []interface{}) (map[string]OptionBasketShare, error) {
	result := make(map[string]OptionBasketShare)
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var entityID string
			var share OptionBasketShare
			if err := rows.Scan(&entityID, &share.OrderCount, &share.OrderPriceSum); err != nil {
				return err
			}
			result[entityID] = share
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get options basket shares: %w", err)
	}
	return result, nil
}
