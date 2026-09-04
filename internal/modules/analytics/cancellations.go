package analytics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
)

// CancellationAuthorUnknown is CancellationAuthorTypeTotal.AuthorType for
// orders.cancelled_by_type IS NULL — never dropped, see models.go.
const CancellationAuthorUnknown = "UNKNOWN"

// CancellationUnattributedUserID is StaffCancellationRow.UserID for the
// synthetic row carrying STAFF-type cancellations whose created_by does not
// match a real users.user_id — see models.go's doc comment.
const CancellationUnattributedUserID = "unattributed"

// staffCancellationMinOrders is the "effectif" floor PROMPT 10 §4 requires
// before a per-server cancellation RATE is shown (raw counts are always
// shown regardless — see StaffCancellationRow's doc comment). Verified
// against staging (PROD scope, 12-month window, 2026-09-04): only 7
// (merchant, server) pairs have any STAFF cancellation at all across the 8
// PROD establishments, and most establishments today run on a single shared
// POS login rather than one account per employee (PERIMETRE.md's low `Users`
// counts per merchant) — so the volumes here are naturally small, and a
// rate computed on a handful of orders would misrepresent a shared account,
// not just an individual. 30 is a deliberately low bar, same posture as
// coversCoverageThreshold (service.go): of the 7 pairs found, 3 sit below
// 30 total orders in the period and would show as counts-only, 4 above it
// would show a rate.
const staffCancellationMinOrders int64 = 30

// staffCancellationRateAvailable gates StaffCancellationRow.RateAvailable —
// factored out as a pure function (mirrors ordersPeriodTotals's
// CoversDataAvailable gate, service.go) so the threshold behavior is unit-
// testable without a database.
func staffCancellationRateAvailable(ordersCreated int64) bool {
	return ordersCreated >= staffCancellationMinOrders
}

// CancellationsTotals is GetCancellationsTotals' aggregate row.
type CancellationsTotals struct {
	CancelledCount         int64
	CancelledAmountCents   int64
	InternalCancelledCount int64
	PlatformCancelledCount int64
	UnknownCancelledCount  int64
	StaffCancelledCount    int64
}

// GetOrdersCreatedCount is the cancellation rate's denominator — every order
// created in the period, see AnalyticsAllOrdersCreatedScope's doc comment
// (scope.go) for why this, not AnalyticsOrdersScope, is the right count.
func (r *Repository) GetOrdersCreatedCount(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) (int64, error) {
	where, args := AnalyticsAllOrdersCreatedScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT COUNT(*)
		FROM orders o
	`) + "\nWHERE " + where

	var count int64
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, args...).Scan(&count)
	})
	if err != nil {
		return 0, fmt.Errorf("get orders created count: %w", err)
	}
	return count, nil
}

// GetCancellationsTotals aggregates the cancellation scope in one pass:
// volume, amount, and the STAFF/CUSTOMER/SYSTEM/PLATFORM/NULL author-type
// partition (via COUNT(*) FILTER, not a second query) — internal is
// STAFF+CUSTOMER+SYSTEM, platform is PLATFORM alone, unknown is
// cancelled_by_type IS NULL (PROMPT 10 §3's central cut, see
// CancellationsPeriodTotals' doc comment).
//
// CancelledAmountCents sums orders.price on the cancelled orders — verified
// against staging before shipping this as a "montant perdu" figure (PROMPT
// 10 §3's explicit caution): PROD scope, CANCELED-only, 12-month window,
// 2026-09-04 — only 4 of 1,310 cancelled orders carry price=0 (0.3%), median
// 1,740 cents / avg 2,161 cents, both in the normal range of a real basket
// on this merchant mix (PERIMETRE.md's per-merchant medians run 12.50€-28.50€).
// Not a mostly-empty-cart artifact — safe to surface as a total.
//
// UnknownCancelledCount, verified the same way: 108/1,461 CANCELED orders on
// PROD (all-time) carry cancelled_by_type IS NULL (~7.4%) — the brief's cited
// 13.4% used a broader "annulation" definition (CANCELED+DENIED+DELETED, see
// scope.go's AnalyticsCancellationsScope doc comment for why this package
// excludes the other two); recomputed here under this tab's own scope
// instead of carried forward unverified.
func (r *Repository) GetCancellationsTotals(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) (CancellationsTotals, error) {
	where, args := AnalyticsCancellationsScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT COUNT(*) AS cancelled_count,
			COALESCE(SUM(o.price), 0) AS amount_cents,
			COUNT(*) FILTER (WHERE o.cancelled_by_type IN ('STAFF', 'CUSTOMER', 'SYSTEM')) AS internal_count,
			COUNT(*) FILTER (WHERE o.cancelled_by_type = 'PLATFORM') AS platform_count,
			COUNT(*) FILTER (WHERE o.cancelled_by_type IS NULL) AS unknown_count,
			COUNT(*) FILTER (WHERE o.cancelled_by_type = 'STAFF') AS staff_count
		FROM orders o
	`) + "\nWHERE " + where

	var totals CancellationsTotals
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, args...).Scan(
			&totals.CancelledCount, &totals.CancelledAmountCents,
			&totals.InternalCancelledCount, &totals.PlatformCancelledCount,
			&totals.UnknownCancelledCount, &totals.StaffCancelledCount,
		)
	})
	if err != nil {
		return CancellationsTotals{}, fmt.Errorf("get cancellations totals: %w", err)
	}
	return totals, nil
}

// reasonSubquerySelect strips the two orders.deletion_reason_id data-quality
// bugs found while building this tab, verified against staging
// (2026-09-04):
//   - stray quote characters baked into the stored value itself (e.g. the
//     literal 3-byte string 'X' — quote, digit, quote — not just the digit),
//     ~230 rows on PROD CANCELED orders. TRIM(BOTH ”” FROM ...) strips
//     them so the value can join deletion_reasons.deletion_reason_id (an
//     integer) at all.
//   - truncation: orders.deletion_reason_id is varchar(11); at least one
//     write path stores a semantic code longer than that ("KIOSK_CUSTO..."
//     observed truncated to exactly 11 bytes) instead of a numeric catalog
//     id. This can never match deletion_reasons — NULLIF/COALESCE below
//     route it to the "uncatalogued" bucket (CancellationReasonTotal's doc
//     comment) rather than silently vanishing from the join.
//
// NULLIF(..., ”) folds both a NULL and an empty-after-trim value to NULL,
// so "no reason recorded" is one case, not two.
const reasonSubquerySelect = `
	SELECT NULLIF(TRIM(BOTH '''' FROM o.deletion_reason_id), '') AS raw_reason_id
	FROM orders o
`

// GetCancellationsByReason groups by orders.deletion_reason_id, joined to
// deletion_reasons for a real French label (deletion_reasons is a
// merchant-agnostic, cross-domain referential — bookings and delivery
// sessions carry their own reasons through the same table, see PERIMETRE.md/
// DROITS.md — but this query is never at risk of leaking one of those in:
// it derives the reason facet from orders actually in scope, joined OUT to
// the catalog, rather than enumerating the catalog and filtering it down.
// Only a reason that some order in this scope actually carries can appear.
//
// Labels come from `labels` (lang='FR', label_type='deletion_reason'), the
// same source internal/modules/pos.POSRepository.GetDeletionReasons already
// uses for the POS's own cancel-reason dropdown — deletion_reasons.
// deletion_reason_desc (used as a fallback here) is the English seed text,
// not what staff actually see. Verified on staging: every reason id that
// shows up on a CANCELED order in the 12-month PROD window has a FR label.
//
// GROUP BY 1, 2 groups by the SELECT list's output columns (reason_id,
// label) — both are already COALESCE-derived, so grouping by their source
// columns instead would fragment identical output rows (e.g. two different
// raw_reason_id values that both fall through to "none").
func (r *Repository) GetCancellationsByReason(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]CancellationReasonTotal, error) {
	where, args := AnalyticsCancellationsScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT
			CASE
				WHEN dr.deletion_reason_id IS NOT NULL THEN dr.deletion_reason_id::text
				WHEN c.raw_reason_id IS NOT NULL THEN 'uncatalogued:' || c.raw_reason_id
				ELSE 'none'
			END AS reason_id,
			COALESCE(l.label, dr.deletion_reason_desc,
				CASE WHEN c.raw_reason_id IS NULL THEN 'Motif non renseigné'
					ELSE 'Motif non catalogué (' || c.raw_reason_id || ')' END) AS label,
			COUNT(*) AS cnt
		FROM (
	`) + "\n\t\t" + strings.TrimSpace(reasonSubquerySelect) + `
			WHERE ` + where + `
		) c
		LEFT JOIN deletion_reasons dr ON dr.deletion_reason_id::text = c.raw_reason_id
		LEFT JOIN labels l ON l.label_value = dr.deletion_reason_id::text
			AND l.label_type = 'deletion_reason' AND l.lang = 'FR'
		GROUP BY 1, 2
		ORDER BY cnt DESC
	`

	var result []CancellationReasonTotal
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row CancellationReasonTotal
			if err := rows.Scan(&row.ReasonID, &row.Label, &row.Count); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get cancellations by reason: %w", err)
	}
	return result, nil
}

// GetCancellationsByAuthorType groups by orders.cancelled_by_type, folding
// NULL to CancellationAuthorUnknown — see models.go's doc comment on why
// this is never silently excluded.
func (r *Repository) GetCancellationsByAuthorType(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]CancellationAuthorTypeTotal, error) {
	where, args := AnalyticsCancellationsScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT COALESCE(o.cancelled_by_type, '`+CancellationAuthorUnknown+`') AS author_type,
			COUNT(*) AS cnt,
			COALESCE(SUM(o.price), 0) AS amount_cents
		FROM orders o
	`) + "\nWHERE " + where + `
		GROUP BY author_type
		ORDER BY cnt DESC
	`

	var result []CancellationAuthorTypeTotal
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row CancellationAuthorTypeTotal
			if err := rows.Scan(&row.AuthorType, &row.Count, &row.AmountCents); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get cancellations by author type: %w", err)
	}
	return result, nil
}

// GetCancellationsByChannel groups by channelCaseExpr (channels.go) — same
// derivation every other tab in this package uses.
func (r *Repository) GetCancellationsByChannel(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]CancellationChannelTotal, error) {
	where, args := AnalyticsCancellationsScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT `+channelCaseExpr+` AS channel,
			COUNT(*) AS cnt,
			COALESCE(SUM(o.price), 0) AS amount_cents
		FROM orders o
	`) + "\nWHERE " + where + `
		GROUP BY channel
		ORDER BY channel
	`

	result := make([]CancellationChannelTotal, 0, len(Channels))
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row CancellationChannelTotal
			if err := rows.Scan(&row.Channel, &row.Count, &row.AmountCents); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get cancellations by channel: %w", err)
	}
	return result, nil
}

// GetCancellationsByStaff is the nominative ranking (POST
// /analytics/cancellations/by-staff, guarded by
// permission.ReportsStaffPerformanceRead — see routes.go). Only
// cancelled_by_type='STAFF' rows count here (PROMPT 10 §4: a customer
// self-service cancellation or a platform cancellation has no server to
// attribute), and only created_by values excluded from '-1'/'SCANNORDER' and
// matched to a real users.user_id row are named — the same join
// stats.ListUpsellByServer already uses for the (unrelated but structurally
// identical) upsell-by-server ranking.
//
// One pass over `orders` computes both the numerator (cancelled_count, via
// FILTER) and the denominator (total_created — every order that user
// created in the period, any brand_status/state, matching
// AnalyticsAllOrdersCreatedScope's definition) per user, so the two numbers
// are guaranteed consistent with each other. HAVING keeps only users with at
// least one STAFF cancellation — this is a cancellations ranking, not a
// staff directory.
//
// A second query appends CancellationUnattributedUserID: STAFF-type
// cancellations whose created_by does NOT match any real users.user_id.
// Verified against staging (PROD scope, 12-month window, 2026-09-04): 120 of
// 1,059 STAFF cancellations (~11.3%) are unattributable this way. Dropping
// them would break PROMPT 10 §6's cross-endpoint coherence check (this
// endpoint's SUM(cancelled_count) must equal the aggregate endpoint's
// ByAuthorType STAFF count) — so they are named as their own row instead,
// with OrdersCreated 0 and RateAvailable always false (there is no
// identifiable person to compute an "effectif" for).
func (r *Repository) GetCancellationsByStaff(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]StaffCancellationRow, error) {
	allWhere, allArgs := AnalyticsAllOrdersCreatedScope(merchantIDs, startUTC, endUTC)
	// The display-name alias is "display_name", not "name": users.name is a
	// real column on the joined table, and GROUP BY resolves a bare "name"
	// to that column instead of this SELECT list's alias — Postgres prefers
	// an actual column over an output alias on a name clash, so `GROUP BY
	// o.created_by, name` fails ("u.first_name must appear in GROUP BY")
	// instead of grouping by the intended expression. Caught by
	// TestCancellations_Postgres against staging while building this query.
	staffQuery := strings.TrimSpace(`
		SELECT o.created_by AS user_id,
			COALESCE(NULLIF(TRIM(CONCAT(COALESCE(u.first_name, ''), ' ', COALESCE(u.last_name, ''))), ''), o.created_by) AS display_name,
			COUNT(*) AS total_created,
			COUNT(*) FILTER (WHERE upper(o.brand_status) = 'CANCELED' AND o.cancelled_by_type = 'STAFF') AS cancelled_count
		FROM orders o
		LEFT JOIN users u ON u.user_id = o.created_by
	`) + "\nWHERE " + allWhere + `
			AND o.created_by NOT IN ('-1', 'SCANNORDER')
			AND EXISTS (SELECT 1 FROM users u2 WHERE u2.user_id = o.created_by)
		GROUP BY o.created_by, display_name
		HAVING COUNT(*) FILTER (WHERE upper(o.brand_status) = 'CANCELED' AND o.cancelled_by_type = 'STAFF') > 0
		ORDER BY cancelled_count DESC, total_created DESC
	`

	cancelWhere, cancelArgs := AnalyticsCancellationsScope(merchantIDs, startUTC, endUTC)
	unattributedQuery := strings.TrimSpace(`
		SELECT COUNT(*)
		FROM orders o
	`) + "\nWHERE " + cancelWhere + `
			AND o.cancelled_by_type = 'STAFF'
			AND NOT EXISTS (SELECT 1 FROM users u2 WHERE u2.user_id = o.created_by)
	`

	var result []StaffCancellationRow
	var unattributedCount int64
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, staffQuery, allArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row StaffCancellationRow
			var totalCreated int64
			if err := rows.Scan(&row.UserID, &row.Name, &totalCreated, &row.CancelledCount); err != nil {
				return err
			}
			row.OrdersCreated = totalCreated
			row.RateAvailable = staffCancellationRateAvailable(totalCreated)
			result = append(result, row)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		return tx.QueryRowContext(ctx, unattributedQuery, cancelArgs...).Scan(&unattributedCount)
	})
	if err != nil {
		return nil, fmt.Errorf("get cancellations by staff: %w", err)
	}

	if unattributedCount > 0 {
		result = append(result, StaffCancellationRow{
			UserID:         CancellationUnattributedUserID,
			Name:           "Non attribuable",
			OrdersCreated:  0,
			CancelledCount: unattributedCount,
			RateAvailable:  false,
		})
	}

	return result, nil
}
