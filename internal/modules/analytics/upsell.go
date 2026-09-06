package analytics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
)

// upsellSuggestionsMinProposed aliases staffCancellationMinOrders
// (cancellations.go) verbatim — same "seuil déjà en usage ailleurs"
// instruction PROMPT 18 gave for minCustomersForRate (service.go), applied
// here to upsell_suggestions.ProposedCount instead of a per-server or
// per-customer effectif. Aliasing rather than copying the literal 30
// guarantees the two can never quietly drift apart.
const upsellSuggestionsMinProposed = staffCancellationMinOrders

// upsellLineHTExpr/upsellLinesFromJoins/upsellLinesWhereClause carry over
// internal/modules/stats.StatsRepository's upsellLineHTExpr/
// upsellLinesFromJoins/upsellLinesWhereClause verbatim — PROMPT 19: "le SQL
// existant est bon ... reprends-le plutôt que de le réécrire," and the
// traceability audit (docs/audits/audit_upsell_traceability.md §G) calls it
// a correctly-wired reference query — with two structural adaptations to fit
// this package's conventions:
//
//   - o.merchant_id = ANY(?) (a Postgres array param) instead of `= ?`, the
//     same form every other query in this package uses — see
//     AnalyticsOrdersScope's doc comment (scope.go).
//   - a channel filter slot, appended by the functions below that need it —
//     the same `AND (channelCaseExpr) = ANY(?)` shape clients.go's
//     GetCustomersCoverage already uses.
//
// What did NOT change: the is_upsell/state/brand_status filter itself is
// byte-for-byte what stats.upsellLinesWhereClause already had — including
// the lack of `upper(o.brand_status)` this package's own
// analyticsOrdersScopeWhere applies elsewhere (scope.go's doc comment: 8 PROD
// rows carry a lowercase brand_status). This is a migration, not a rewrite:
// since orderitems.is_upsell is false on every row in this system today (see
// models.go's package doc comment), that divergence is currently
// unobservable on either side of the migration — worth revisiting once the
// Kiosk/ScanNOrder write-path gap closes (a separate, later lot), not now.
const upsellLineHTExpr = `
	CASE
		WHEN tva.tva_rate = 0 THEN ((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity)
		ELSE ((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity) * 100.0 / (100.0 + tva.tva_rate)
	END
`

const upsellLinesFromJoins = `
	FROM orderitems oi
	INNER JOIN orders o ON o.order_id = oi.order_id
	INNER JOIN products p ON p.product_id = oi.product_id
	INNER JOIN tva_categories tva ON tva.tva_id = (
		CASE
			WHEN o.order_type = 'DELIVERY' THEN p.tva_delivery_id
			WHEN o.order_type = 'TAKE_AWAY' THEN p.tva_take_away_id
			ELSE p.tva_in_id
		END
	)
	LEFT JOIN (
		SELECT order_item_id, SUM(extra.price) AS extra_price
		FROM extra
		GROUP BY order_item_id
	) e ON e.order_item_id = oi.order_item_id
`

const upsellLinesWhereClause = `
	WHERE oi.is_upsell = true
	AND o.merchant_id = ANY(?)
	AND o.creation_date >= ?
	AND o.creation_date < ?
	AND o.state IN ('CLOSED', 'DONE')
	AND o.brand_status NOT IN ('DELETED', 'CANCELED')
`

// UpsellTotals is GetUpsellTotals' aggregate row.
type UpsellTotals struct {
	UpsellLines          int64
	UpsellRevenueHTCents int64
}

// GetUpsellTotals mirrors stats.StatsRepository.GetUpsellTotals, plus the
// channel filter this package's Clients/Products/Options tabs already apply.
func (r *Repository) GetUpsellTotals(ctx context.Context, merchantIDs, channels []string, startUTC, endUTC time.Time) (UpsellTotals, error) {
	query := strings.TrimSpace(`
		SELECT COUNT(*) AS total_lines,
			`+roundToIntExpr("COALESCE(SUM("+upsellLineHTExpr+"), 0)")+` AS revenue_ht
	`) + "\n" + strings.TrimSpace(upsellLinesFromJoins) + "\n" +
		strings.TrimSpace(upsellLinesWhereClause) + ` AND (` + channelCaseExpr + `) = ANY(?)`

	var totals UpsellTotals
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, merchantIDs, startUTC, endUTC, channels).
			Scan(&totals.UpsellLines, &totals.UpsellRevenueHTCents)
	})
	if err != nil {
		return UpsellTotals{}, fmt.Errorf("get upsell totals: %w", err)
	}
	return totals, nil
}

// GetOrdersWithUpsellCount mirrors stats.StatsRepository.GetOrdersWithUpsellCount:
// the number of distinct orders carrying at least one is_upsell line, within
// the same scope/channel filter GetUpsellTotals uses.
func (r *Repository) GetOrdersWithUpsellCount(ctx context.Context, merchantIDs, channels []string, startUTC, endUTC time.Time) (int64, error) {
	query := strings.TrimSpace(`
		SELECT COUNT(DISTINCT o.order_id)
	`) + "\n" + strings.TrimSpace(upsellLinesFromJoins) + "\n" +
		strings.TrimSpace(upsellLinesWhereClause) + ` AND (` + channelCaseExpr + `) = ANY(?)`

	var count int64
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, merchantIDs, startUTC, endUTC, channels).Scan(&count)
	})
	if err != nil {
		return 0, fmt.Errorf("get orders with upsell count: %w", err)
	}
	return count, nil
}

// GetUpsellOrdersTotal is the tab's rate denominator: every order in this
// package's canonical AnalyticsOrdersScope, restricted to the requested
// channel filter — the same scope/channel combination clients.go's
// GetCustomersCoverage already uses for its own denominator. Does not depend
// on is_upsell at all, so this number is real regardless of
// UpsellResponse.InstrumentationActive.
func (r *Repository) GetUpsellOrdersTotal(ctx context.Context, merchantIDs, channels []string, startUTC, endUTC time.Time) (int64, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT COUNT(*) FROM orders o
	`) + "\nWHERE " + where + ` AND (` + channelCaseExpr + `) = ANY(?)`
	args = append(args, channels)

	var count int64
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, args...).Scan(&count)
	})
	if err != nil {
		return 0, fmt.Errorf("get upsell orders total: %w", err)
	}
	return count, nil
}

// GetUpsellByStaff mirrors stats.StatsRepository.ListUpsellByServer — same
// self-service exclusion (a ScanNOrder/no-user order has no server to
// credit), same display-name fallback chain as cancellations.go's
// GetCancellationsByStaff.
func (r *Repository) GetUpsellByStaff(ctx context.Context, merchantIDs, channels []string, startUTC, endUTC time.Time) ([]UpsellStaffRow, error) {
	// display_name, not name: products (joined via upsellLinesFromJoins)
	// already has its own `name` column, so a bare `name` alias here is
	// ambiguous to Postgres inside GROUP BY — same clash class as
	// cancellations.go's GetCancellationsByStaff doc comment describes for
	// users.name, just against a different table this time.
	query := strings.TrimSpace(`
		SELECT o.created_by AS user_id,
			COALESCE(NULLIF(TRIM(CONCAT(COALESCE(u.first_name, ''), ' ', COALESCE(u.last_name, ''))), ''), o.created_by) AS display_name,
			COUNT(*) AS upsell_lines,
			`+roundToIntExpr("COALESCE(SUM("+upsellLineHTExpr+"), 0)")+` AS upsell_revenue_ht
	`) + "\n" + strings.TrimSpace(upsellLinesFromJoins) + `
		LEFT JOIN users u ON u.user_id = o.created_by
	` + strings.TrimSpace(upsellLinesWhereClause) + `
		AND (` + channelCaseExpr + `) = ANY(?)
		AND o.created_by NOT IN ('-1', 'SCANNORDER')
		GROUP BY o.created_by, display_name
		ORDER BY upsell_revenue_ht DESC
	`

	var result []UpsellStaffRow
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, merchantIDs, startUTC, endUTC, channels)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row UpsellStaffRow
			if err := rows.Scan(&row.UserID, &row.Name, &row.UpsellLines, &row.UpsellRevenueHTCents); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get upsell by staff: %w", err)
	}
	return result, nil
}

// GetUpsellInstrumentationActive is the switch PROMPT 19 asks for: true once
// at least one orderitems row for this merchant scope has ever carried
// is_upsell = true, across every period, every state, every brand_status —
// deliberately unbounded by date or scope beyond merchant_id, since the
// question is "has this establishment's write path ever produced this
// signal at all," not "did it happen in the requested window." Flips to
// true automatically, with no redeploy, the moment any channel starts
// writing is_upsell = true on a real order line (currently only POS does —
// see docs/audits/audit_upsell_traceability.md: Kiosk/ScanNOrder both have
// working upsell UIs but neither serializes the flag yet, a separate later
// lot this PROMPT does not touch).
func (r *Repository) GetUpsellInstrumentationActive(ctx context.Context, merchantIDs []string) (bool, error) {
	var active bool
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM orderitems
				WHERE merchant_id = ANY(?) AND is_upsell = true
			)
		`, merchantIDs).Scan(&active)
	})
	if err != nil {
		return false, fmt.Errorf("get upsell instrumentation active: %w", err)
	}
	return active, nil
}

// GetUpsellSuggestionsTotals reads upsell_suggestions directly — see
// UpsellSuggestionsTotals' doc comment (models.go) for why this is a
// separate, already-working signal, independent of orderitems.is_upsell.
// Deliberately not channel-filtered: upsell_suggestions.channel is
// POS/SNO/KIOSK, a different taxonomy from channelCaseExpr's dine_in/
// takeaway/... keys, and a proposed-but-never-accepted suggestion carries no
// order_id to derive a channelCaseExpr channel from in the first place.
func (r *Repository) GetUpsellSuggestionsTotals(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) (proposed, accepted int64, err error) {
	err = r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, `
			SELECT COUNT(*),
				COUNT(*) FILTER (WHERE accepted_items IS NOT NULL)
			FROM upsell_suggestions
			WHERE merchant_id = ANY(?)
			AND created_at >= ?
			AND created_at < ?
		`, merchantIDs, startUTC, endUTC).Scan(&proposed, &accepted)
	})
	if err != nil {
		return 0, 0, fmt.Errorf("get upsell suggestions totals: %w", err)
	}
	return proposed, accepted, nil
}
