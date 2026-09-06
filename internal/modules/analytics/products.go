package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
)

// ---- Produits (POST /analytics/products) ----
//
// This file's queries are the direct answer to PROMPT 16 §4's performance
// requirement: the maquette's query (docs/analytics/AUDIT.md, wello-back-
// office repo, "M2 — Mix produits + HT + suppléments") measured 1 347 ms and
// read 120 433 orderitems rows to render 1 239 — five sequential passes
// building one screen. Every query below reads orderitems exactly once:
// GetProductsPage's single CTE both computes every per-product aggregate AND
// (via COUNT(*) OVER()) the pagination total, so paging never re-runs the
// GROUP BY. GetProductsScopeTotals is a second, ungrouped single pass (cheap:
// SUM/COUNT, no per-product grouping) for the tab's KPI tiles and aggregate
// margin. GetProductsPreviousRevenue is bounded to the current page's
// product_ids (at most ProductsMaxPageSize), never the full previous-period
// catalog. GetProductCategories reads productcateg only, indexed by
// merchant_id. Four queries total, none of them re-reading orderitems for
// data the first pass already produced.

// GetProductCategories returns every enabled category for the accessible
// merchant scope, ordered the same way the POS/menu screens order them
// (categ_order) — read from productcateg, never the maquette's hardcoded
// entrees/plats/desserts/boissons list (PROMPT 16 §3).
func (r *Repository) GetProductCategories(ctx context.Context, merchantIDs []string) ([]ProductCategoryOption, error) {
	query := `
		SELECT merchant_categ_id, categ_name
		FROM productcateg
		WHERE merchant_id = ANY(?) AND enabled = TRUE
		ORDER BY categ_order ASC
	`

	result := make([]ProductCategoryOption, 0)
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, merchantIDs)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row ProductCategoryOption
			if err := rows.Scan(&row.CategoryID, &row.Name); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get product categories: %w", err)
	}
	return result, nil
}

// productsCategoryFilter appends the optional category_id filter to a
// products-scoped WHERE built from AnalyticsOrdersScope — categoryID empty
// means every category, matching every other optional filter in this
// package's convention (e.g. paymentsScopeJoin).
func productsCategoryFilter(categoryID string, args []interface{}) (string, []interface{}) {
	if categoryID == "" {
		return "", args
	}
	return " AND p.category = ?", append(args, categoryID)
}

// ProductsScopeTotals is the ungrouped aggregate behind ProductsPeriodTotals
// and ProductsCostCoverage — see this file's doc comment for why it is a
// second, cheap single pass rather than derived from GetProductsPage (which
// is paginated and therefore never sees the whole scope at once).
type ProductsScopeTotals struct {
	QuantitySold             int64
	RevenueTTCCents          int64
	RevenueHTCents           int64
	CostKnownRevenueTTCCents int64
	CostPriceCents           int64
	NoRecipeQuantity         int64
	IncompleteRecipeQuantity int64
}

// GetProductsScopeTotals sums quantity/TTC/HT across every product line in
// scope, plus the cost-known subset's own revenue and cost — the two numbers
// ProductsCostCoverage needs to compute a margin without ever dividing a
// complete revenue sum by a partial cost sum (see this package's models.go
// doc comment on ProductsCostCoverage).
func (r *Repository) GetProductsScopeTotals(ctx context.Context, merchantIDs []string, categoryID string, startUTC, endUTC time.Time) (ProductsScopeTotals, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	categoryFilter, args := productsCategoryFilter(categoryID, args)

	query := strings.TrimSpace(`
		SELECT
			COALESCE(SUM(oi.quantity), 0),
			COALESCE(SUM((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity), 0),
			`+roundToIntExpr("COALESCE(SUM("+htLineExpr+"), 0)")+`,
			COALESCE(SUM((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity) FILTER (WHERE oi.cost_price_unit IS NOT NULL), 0),
			COALESCE(SUM(oi.cost_price_unit * oi.quantity) FILTER (WHERE oi.cost_price_unit IS NOT NULL), 0),
			COALESCE(SUM(oi.quantity) FILTER (WHERE oi.cost_price_unit IS NULL AND oi.cost_price_reason = 'NO_RECIPE'), 0),
			COALESCE(SUM(oi.quantity) FILTER (WHERE oi.cost_price_unit IS NULL AND oi.cost_price_reason = 'INCOMPLETE_RECIPE'), 0)
	`) + "\n" + htLineJoins + "\nWHERE " + where + categoryFilter

	var totals ProductsScopeTotals
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, args...).Scan(
			&totals.QuantitySold, &totals.RevenueTTCCents, &totals.RevenueHTCents,
			&totals.CostKnownRevenueTTCCents, &totals.CostPriceCents,
			&totals.NoRecipeQuantity, &totals.IncompleteRecipeQuantity,
		)
	})
	if err != nil {
		return ProductsScopeTotals{}, fmt.Errorf("get products scope totals: %w", err)
	}
	return totals, nil
}

// ProductAggRow is GetProductsPage's raw row — CostPriceCents is nullable
// (NULL when no line for this product carried a known cost_price_unit),
// mapped to ProductRow's nil-means-unknown fields in service.go, never a
// silent 0.
type ProductAggRow struct {
	ProductID                string
	Name                     string
	CategoryID               string
	CategoryName             string
	QuantitySold             int64
	RevenueTTCCents          int64
	RevenueHTCents           int64
	CostKnownQuantity        int64
	CostKnownRevenueTTCCents int64
	CostPriceCents           sql.NullInt64
	NoRecipeQuantity         int64
	IncompleteRecipeQuantity int64
}

// productsSortColumn whitelists ProductsRequest.SortBy into a bare output
// column name from the CTE below — never string-interpolating the client's
// value itself into ORDER BY, the same discipline bookings.repository.go's
// sort whitelist already applies.
func productsSortColumn(sortBy string) string {
	switch sortBy {
	case ProductsSortRevenue:
		return "revenue_ttc_cents"
	case ProductsSortMargin:
		return "margin_cents"
	default:
		return "quantity_sold"
	}
}

// GetProductsPage returns one page of per-product aggregates for the current
// period, sorted in SQL (never client-side — PROMPT 16 §3), plus the total
// number of distinct products matching the scope (via COUNT(*) OVER(),
// computed in the same pass as the aggregation itself — see this file's doc
// comment on why that avoids a second GROUP BY just to count).
//
// margin_cents is derived in the outer SELECT from the CTE's own
// cost_price_cents/cost_known_revenue_ttc_cents columns (a CASE reading two
// already-materialized aggregate outputs), not recomputed with a second
// FILTER — Postgres lets a query after a CTE reference its output columns
// directly.
//
// revenue_ht_cents is rounded per product (roundToIntExpr on this group's own
// SUM), NOT apportioned against a separately-computed ungrouped total the way
// VATRateTotal/VATChannelTotal are (apportion.go) — PROMPT 16's verification
// only requires the per-product TTC column to sum exactly to the CA tab's
// total (TTC is a raw integer sum, no division involved, so it always does).
// HT is derived via a per-line division, so a page's per-product HT values
// can drift from the ungrouped total by at most a few cents across the whole
// result set — the same ordinary rounding drift already accepted elsewhere
// in this codebase for a non-fiscal breakdown (see CancellationsResponse's
// doc comment, models.go).
func (r *Repository) GetProductsPage(ctx context.Context, merchantIDs []string, categoryID, sortBy, sortDir string, page, pageSize int, startUTC, endUTC time.Time) ([]ProductAggRow, int64, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	categoryFilter, args := productsCategoryFilter(categoryID, args)

	sortColumn := productsSortColumn(sortBy)
	dir := "DESC"
	if strings.EqualFold(sortDir, "asc") {
		dir = "ASC"
	}
	// margin_cents is NULL for an uncosted product — NULLS LAST regardless of
	// direction, so an unpriced product never floats to the top of a
	// margin sort by accident of Postgres's default NULL ordering (which
	// differs between ASC and DESC).
	nullsClause := ""
	if sortColumn == "margin_cents" {
		nullsClause = " NULLS LAST"
	}

	args = append(args, pageSize, (page-1)*pageSize)

	query := strings.TrimSpace(`
		WITH product_agg AS (
			SELECT
				p.product_id::text AS product_id,
				p.name AS name,
				p.category AS category_id,
				COALESCE(pc.categ_name, '') AS category_name,
				SUM(oi.quantity) AS quantity_sold,
				SUM((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity) AS revenue_ttc_cents,
				`+roundToIntExpr("SUM("+htLineExpr+")")+` AS revenue_ht_cents,
				COALESCE(SUM(oi.quantity) FILTER (WHERE oi.cost_price_unit IS NOT NULL), 0) AS cost_known_quantity,
				COALESCE(SUM((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity) FILTER (WHERE oi.cost_price_unit IS NOT NULL), 0) AS cost_known_revenue_ttc_cents,
				SUM(oi.cost_price_unit * oi.quantity) FILTER (WHERE oi.cost_price_unit IS NOT NULL) AS cost_price_cents,
				COALESCE(SUM(oi.quantity) FILTER (WHERE oi.cost_price_unit IS NULL AND oi.cost_price_reason = 'NO_RECIPE'), 0) AS no_recipe_quantity,
				COALESCE(SUM(oi.quantity) FILTER (WHERE oi.cost_price_unit IS NULL AND oi.cost_price_reason = 'INCOMPLETE_RECIPE'), 0) AS incomplete_recipe_quantity
	`) + "\n\t\t" + strings.TrimSpace(htLineJoins) + `
			LEFT JOIN productcateg pc ON pc.merchant_categ_id = p.category AND pc.merchant_id = p.merchant_id
			WHERE ` + where + categoryFilter + `
			GROUP BY p.product_id, p.name, p.category, pc.categ_name
		)
		SELECT
			product_id, name, category_id, category_name,
			quantity_sold, revenue_ttc_cents, revenue_ht_cents,
			cost_known_quantity, cost_known_revenue_ttc_cents, cost_price_cents,
			CASE WHEN cost_price_cents IS NOT NULL THEN cost_known_revenue_ttc_cents - cost_price_cents ELSE NULL END AS margin_cents,
			no_recipe_quantity, incomplete_recipe_quantity,
			COUNT(*) OVER() AS total_products
		FROM product_agg
		ORDER BY ` + sortColumn + ` ` + dir + nullsClause + `, product_id ASC
		LIMIT ? OFFSET ?
	`

	var result []ProductAggRow
	var totalProducts int64
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row ProductAggRow
			var marginCents sql.NullInt64
			if err := rows.Scan(
				&row.ProductID, &row.Name, &row.CategoryID, &row.CategoryName,
				&row.QuantitySold, &row.RevenueTTCCents, &row.RevenueHTCents,
				&row.CostKnownQuantity, &row.CostKnownRevenueTTCCents, &row.CostPriceCents,
				&marginCents,
				&row.NoRecipeQuantity, &row.IncompleteRecipeQuantity,
				&totalProducts,
			); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, fmt.Errorf("get products page: %w", err)
	}
	return result, totalProducts, nil
}

// GetProductsPreviousRevenue returns the previous period's TTC revenue for
// exactly productIDs (the current page's rows, never the full catalog) —
// ProductRow.EvolutionPercent's data source. A product absent from the
// returned map had zero previous-period sales; service.go treats that as
// "no evolution to show" (nil), never a divide-by-zero.
func (r *Repository) GetProductsPreviousRevenue(ctx context.Context, merchantIDs []string, productIDs []string, startUTC, endUTC time.Time) (map[string]int64, error) {
	result := make(map[string]int64, len(productIDs))
	if len(productIDs) == 0 {
		return result, nil
	}

	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT p.product_id::text,
			SUM((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity)
	`) + "\n" + htLineJoins + `
		WHERE ` + where + ` AND p.product_id::text = ANY(?)
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
			var revenueTTCCents int64
			if err := rows.Scan(&productID, &revenueTTCCents); err != nil {
				return err
			}
			result[productID] = revenueTTCCents
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get products previous revenue: %w", err)
	}
	return result, nil
}
