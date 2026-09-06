package analytics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
)

// ---- Remises (POST /analytics/discounts) ----
//
// PROMPT 22. Every query here reads discount_redemptions (PROMPT 21's table
// de liaison), joined to orders for AnalyticsOrdersScope/channelCaseExpr —
// discount_redemptions carries merchant_id but no period/state/channel
// information of its own, so the join is never optional. discounts is
// LEFT JOINed only for display (current name, enabled flag): the FK
// discount_redemptions.discount_id -> discounts.discount_id_new has no
// ON DELETE and discounts is only ever soft-deleted (enabled=false, never
// removed), so the join can never actually miss — LEFT rather than INNER is
// defensive, not load-bearing. See models.go's package doc comment for the
// three ways this tab differs from the maquette it replaces.

// discountRedemptionsScopeJoin is shared by every query in this file: the
// same AnalyticsOrdersScope every other tab uses, plus the caller's channel
// filter, applied to discount_redemptions joined to orders on order_id.
func discountRedemptionsScopeJoin(merchantIDs, channels []string, startUTC, endUTC time.Time) (string, []interface{}) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	where += "\n\t\tAND (" + channelCaseExpr + ") = ANY(?)"
	args = append(args, channels)
	return where, args
}

// DiscountsScopeTotals is GetDiscountsScopeTotals' raw row — the whole
// scope's aggregate, not just the current page, mirroring
// ProductsScopeTotals/OptionsScopeTotals' role for their own tabs.
type DiscountsScopeTotals struct {
	TotalAmountCents              int64
	ReconstructedAmountCents      int64
	MeasuredAmountCents           int64
	ReconstructedRedemptionsCount int64
	MeasuredRedemptionsCount      int64
	DiscountedOrdersCount         int64
}

// GetDiscountsScopeTotals sums every redemption in scope in one pass,
// including the reconstructed/measured split (COUNT/SUM FILTER, not a second
// query) this tab's whole "floor vs complete" framing depends on — see
// models.go's package doc comment.
func (r *Repository) GetDiscountsScopeTotals(ctx context.Context, merchantIDs, channels []string, startUTC, endUTC time.Time) (DiscountsScopeTotals, error) {
	where, args := discountRedemptionsScopeJoin(merchantIDs, channels, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT
			COALESCE(SUM(dr.amount_applied_cents), 0),
			COALESCE(SUM(dr.amount_applied_cents) FILTER (WHERE dr.is_reconstructed), 0),
			COALESCE(SUM(dr.amount_applied_cents) FILTER (WHERE NOT dr.is_reconstructed), 0),
			COUNT(*) FILTER (WHERE dr.is_reconstructed),
			COUNT(*) FILTER (WHERE NOT dr.is_reconstructed),
			COUNT(DISTINCT dr.order_id)
		FROM discount_redemptions dr
		INNER JOIN orders o ON o.order_id = dr.order_id
	`) + "\nWHERE " + where

	var totals DiscountsScopeTotals
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, args...).Scan(
			&totals.TotalAmountCents, &totals.ReconstructedAmountCents, &totals.MeasuredAmountCents,
			&totals.ReconstructedRedemptionsCount, &totals.MeasuredRedemptionsCount,
			&totals.DiscountedOrdersCount,
		)
	})
	if err != nil {
		return DiscountsScopeTotals{}, fmt.Errorf("get discounts scope totals: %w", err)
	}
	return totals, nil
}

// DiscountsOrdersTotals is GetDiscountsOrdersTotals' raw row — the
// DiscountRatePercent/OrdersWithDiscountRatePercent denominators, computed
// under the exact same AnalyticsOrdersScope+channel filter as every other
// number in this response (never a bare COUNT(*) that could drift from the
// scope discount_redemptions itself is read under).
type DiscountsOrdersTotals struct {
	TotalOrdersCount         int64
	ReferenceRevenueTTCCents int64
}

// GetDiscountsOrdersTotals counts every order in scope (discounted or not)
// and sums their TTC — orders.price is already net of any discount applied,
// so ReferenceRevenueTTCCents is "what was actually encashed," the
// denominator DiscountsPeriodTotals.DiscountRatePercent's doc comment
// commits to.
func (r *Repository) GetDiscountsOrdersTotals(ctx context.Context, merchantIDs, channels []string, startUTC, endUTC time.Time) (DiscountsOrdersTotals, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	where += "\n\t\tAND (" + channelCaseExpr + ") = ANY(?)"
	args = append(args, channels)

	query := strings.TrimSpace(`
		SELECT COUNT(*), COALESCE(SUM(o.price), 0)
		FROM orders o
	`) + "\nWHERE " + where

	var totals DiscountsOrdersTotals
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, args...).Scan(&totals.TotalOrdersCount, &totals.ReferenceRevenueTTCCents)
	})
	if err != nil {
		return DiscountsOrdersTotals{}, fmt.Errorf("get discounts orders totals: %w", err)
	}
	return totals, nil
}

// DiscountsMarginCoverageTotals is GetDiscountsMarginCoverage's raw row — see
// DiscountsMarginCoverage's doc comment (models.go) for why this is scoped to
// PRODUCT_LINE redemptions only, and why the fields it feeds are nil below
// coversCoverageThreshold in practice today.
type DiscountsMarginCoverageTotals struct {
	RevenueTTCCentsTotal   int64
	RevenueTTCCentsCovered int64
	DiscountCentsCovered   int64
	CostCentsCovered       int64
}

// GetDiscountsMarginCoverage joins discount_redemptions (scope=PRODUCT_LINE
// only — see models.go) to orderitems on order_item_id for cost_price_unit,
// same nullable-cost convention as GetProductsScopeTotals: both the revenue
// and the discount/cost sums are restricted to exactly the same cost-known
// subset, never a complete sum divided by a partial one.
func (r *Repository) GetDiscountsMarginCoverage(ctx context.Context, merchantIDs, channels []string, startUTC, endUTC time.Time) (DiscountsMarginCoverageTotals, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	where += "\n\t\tAND (" + channelCaseExpr + ") = ANY(?)"
	args = append(args, channels)

	query := strings.TrimSpace(`
		SELECT
			COALESCE(SUM((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity), 0),
			COALESCE(SUM((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity) FILTER (WHERE oi.cost_price_unit IS NOT NULL), 0),
			COALESCE(SUM(dr.amount_applied_cents) FILTER (WHERE oi.cost_price_unit IS NOT NULL), 0),
			COALESCE(SUM(oi.cost_price_unit * oi.quantity) FILTER (WHERE oi.cost_price_unit IS NOT NULL), 0)
		FROM discount_redemptions dr
		INNER JOIN orders o ON o.order_id = dr.order_id
		INNER JOIN orderitems oi ON oi.order_item_id = dr.order_item_id
		LEFT JOIN (
			SELECT order_item_id, SUM(extra.price) AS extra_price
			FROM extra
			GROUP BY order_item_id
		) e ON e.order_item_id = oi.order_item_id
	`) + "\nWHERE dr.scope = 'PRODUCT_LINE' AND " + where

	var totals DiscountsMarginCoverageTotals
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, args...).Scan(
			&totals.RevenueTTCCentsTotal, &totals.RevenueTTCCentsCovered,
			&totals.DiscountCentsCovered, &totals.CostCentsCovered,
		)
	})
	if err != nil {
		return DiscountsMarginCoverageTotals{}, fmt.Errorf("get discounts margin coverage: %w", err)
	}
	return totals, nil
}

// GetDiscountsMeasurementCompleteFrom returns the earliest created_at among
// live-written (is_reconstructed=false) rows for this merchant scope, with
// no period bound — DiscountsResponse.MeasurementCompleteFrom is a global
// fact about this establishment's data, not something that changes per
// report window. nil (ok=false) when no live write exists yet.
func (r *Repository) GetDiscountsMeasurementCompleteFrom(ctx context.Context, merchantIDs []string) (time.Time, bool, error) {
	query := `
		SELECT MIN(dr.created_at)
		FROM discount_redemptions dr
		WHERE dr.merchant_id = ANY(?) AND NOT dr.is_reconstructed
	`

	var earliest *time.Time
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, merchantIDs).Scan(&earliest)
	})
	if err != nil {
		return time.Time{}, false, fmt.Errorf("get discounts measurement complete from: %w", err)
	}
	if earliest == nil {
		return time.Time{}, false, nil
	}
	return *earliest, true, nil
}

// discountsSortColumn whitelists DiscountsRequest.SortBy — never string-
// interpolating the client's own value into ORDER BY, same discipline as
// productsSortColumn/optionsSortColumn.
func discountsSortColumn(sortBy string) string {
	if sortBy == DiscountsSortCount {
		return "redemptions_count"
	}
	return "total_amount_cents"
}

// GetDiscountsPage returns one page of per-discount aggregates (répartition
// par remise), sorted and counted server-side — grouped by dr.discount_id
// alone (never discount_name/discount_code, see DiscountRow's doc comment),
// so a rename mid-period cannot fragment one discount's history into two
// rows. LEFT JOIN discounts for display only (see this file's doc comment on
// why it is never actually missing in practice).
func (r *Repository) GetDiscountsPage(ctx context.Context, merchantIDs, channels []string, sortBy, sortDir string, page, pageSize int, startUTC, endUTC time.Time) ([]DiscountRow, int64, error) {
	where, args := discountRedemptionsScopeJoin(merchantIDs, channels, startUTC, endUTC)

	sortColumn := discountsSortColumn(sortBy)
	dir := "DESC"
	if strings.EqualFold(sortDir, "asc") {
		dir = "ASC"
	}

	query := strings.TrimSpace(`
		SELECT
			discount_id, discount_name, is_deleted,
			total_amount_cents, redemptions_count,
			reconstructed_amount_cents, measured_amount_cents,
			COUNT(*) OVER() AS total_rows
		FROM (
			SELECT
				dr.discount_id,
				COALESCE(d.discount_name, 'Remise #' || dr.discount_id::text) AS discount_name,
				NOT COALESCE(d.enabled, true) AS is_deleted,
				SUM(dr.amount_applied_cents) AS total_amount_cents,
				COUNT(*) AS redemptions_count,
				COALESCE(SUM(dr.amount_applied_cents) FILTER (WHERE dr.is_reconstructed), 0) AS reconstructed_amount_cents,
				COALESCE(SUM(dr.amount_applied_cents) FILTER (WHERE NOT dr.is_reconstructed), 0) AS measured_amount_cents
			FROM discount_redemptions dr
			INNER JOIN orders o ON o.order_id = dr.order_id
			LEFT JOIN discounts d ON d.discount_id_new = dr.discount_id
	`) + "\n\t\tWHERE " + where + `
			GROUP BY dr.discount_id, d.discount_name, d.enabled
		) agg
		ORDER BY ` + sortColumn + ` ` + dir + `, discount_id ASC
		LIMIT ? OFFSET ?
	`

	args = append(args, pageSize, (page-1)*pageSize)

	var result []DiscountRow
	var totalRows int64
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row DiscountRow
			if err := rows.Scan(
				&row.DiscountID, &row.DiscountName, &row.IsDeleted,
				&row.TotalAmountCents, &row.RedemptionsCount,
				&row.ReconstructedAmountCents, &row.MeasuredAmountCents,
				&totalRows,
			); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, fmt.Errorf("get discounts page: %w", err)
	}
	return result, totalRows, nil
}
