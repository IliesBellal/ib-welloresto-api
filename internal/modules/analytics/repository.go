package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database"
	"welloresto-api/internal/database/dbx"
)

// Repository runs every analytics query against the dedicated low-priority
// pool (database.NewAnalyticsPostgres) — never selectedDB, the POS pool.
type Repository struct {
	db *sql.DB
}

func NewRepository(analyticsDB *sql.DB) *Repository {
	return &Repository{db: analyticsDB}
}

// runTx opens one read-only transaction per request, applies the fusible
// (statement_timeout + work_mem, both SET LOCAL — never global, see
// database.AnalyticsStatementTimeoutMS/AnalyticsWorkMemMB's doc comments for
// why), and runs fn inside it. Every exported Repository method funnels
// through this — there is no query path in this package that skips the
// fusible.
func (r *Repository) runTx(ctx context.Context, fn func(ctx context.Context, db *dbx.DB) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin analytics tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once Commit has succeeded

	// SET LOCAL only accepts a literal, not a bind parameter — safe here
	// because both values are Go constants (database package), never
	// request-derived.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", database.AnalyticsStatementTimeoutMS)); err != nil {
		return fmt.Errorf("set analytics statement_timeout: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL work_mem = '%dMB'", database.AnalyticsWorkMemMB)); err != nil {
		return fmt.Errorf("set analytics work_mem: %w", err)
	}

	if err := fn(ctx, dbx.Wrap(tx)); err != nil {
		return err
	}
	return tx.Commit()
}

// GetMerchantTimezone mirrors stats.StatsRepository.GetMerchantTimezone —
// duplicated rather than imported to keep this package's only dependency on
// another module being auth (for UserLoginRow), consistent with how
// pos/reports and stats each already hold their own copy of small
// single-purpose queries like this one.
func (r *Repository) GetMerchantTimezone(ctx context.Context, merchantID string) (string, error) {
	var timezone string
	err := dbx.GetDB(ctx, r.db).QueryRowContext(ctx,
		`SELECT timezone FROM merchant WHERE id = ?`, merchantID,
	).Scan(&timezone)
	if err != nil {
		return "", fmt.Errorf("get merchant timezone: %w", err)
	}
	return timezone, nil
}

// RevenueTotals is the TTC/HT/order-count total for one period.
type RevenueTotals struct {
	TotalTTCCents int64
	TotalHTCents  int64
	OrderCount    int64
}

// GetRevenueTotalsTTC sums TTC (orders.price) and counts orders in scope.
// orders.price is reliable across all brands — see repository doc on
// GetRevenueTotalsHT for why HT needs a separate, more expensive query.
func (r *Repository) GetRevenueTotalsTTC(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) (RevenueTotals, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT COALESCE(SUM(o.price), 0), COUNT(*)
		FROM orders o
	`) + "\nWHERE " + where

	var totals RevenueTotals
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, args...).Scan(&totals.TotalTTCCents, &totals.OrderCount)
	})
	if err != nil {
		return RevenueTotals{}, fmt.Errorf("get revenue totals TTC: %w", err)
	}
	return totals, nil
}

// roundToIntExpr wraps a fractional SQL expression so ROUND() accepts it:
// Postgres's two-argument ROUND only accepts numeric, and tva_rate (real)
// forces float arithmetic without an explicit cast. Same fragment as
// stats.roundToIntExpr — duplicated for the same reason as
// GetMerchantTimezone above.
func roundToIntExpr(expr string) string {
	return "ROUND(CAST(" + expr + " AS numeric), 0)"
}

// htLineExpr computes one order line's HT from its TTC and the product's TVA
// rate for the order's service type — identical shape to
// pos/reports.GetTVAReportData and stats.upsellLineHTExpr (product →
// tva_delivery_id/tva_take_away_id/tva_in_id depending on order_type).
const htLineExpr = `
	CASE
		WHEN tva.tva_rate = 0 THEN ((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity)
		ELSE ((oi.price + COALESCE(e.extra_price, 0)) * oi.quantity) * 100.0 / (100.0 + tva.tva_rate)
	END
`

const htLineJoins = `
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

// deliveryFeeHTExpr/deliveryFeeJoins are the VAT tab's second line source —
// orders.delivery_fees, a flat order-level TTC fee that never appears in
// orderitems (htLineExpr/htLineJoins above never reach it). Same CASE shape
// as htLineExpr, applied to the fee instead of a product line, using
// tva_id=-1's own tva_rate rather than a hardcoded 20% so this keeps working
// if that rate is ever edited.
//
// tva_id=-1 is enabled=false, show_in_report=false (P18, verified against
// staging) — deliberately joined unconditionally on both flags anyway, for
// two independent reasons: (1) htLineJoins above already joins
// tva_categories unconditionally on enabled/show_in_report for product
// lines, so this stays consistent with that; (2) pos/reports.GetTVAReportData
// (internal/modules/pos/reports/repository.go) does the same for its own
// delivery-fee UNION ALL branch — matching it here means the two endpoints'
// delivery-fee handling stays explicable against each other, not a second,
// diverging way of interpreting the same disabled-but-live category.
const deliveryFeeHTExpr = `
	CASE
		WHEN tva_fees.tva_rate = 0 THEN o.delivery_fees
		ELSE o.delivery_fees * 100.0 / (100.0 + tva_fees.tva_rate)
	END
`

const deliveryFeeJoins = `
	FROM orders o
	INNER JOIN tva_categories tva_fees ON tva_fees.tva_id = -1
`

// deliveryFeeFilter excludes zero-fee orders from the UNION ALL branch — a
// 0 fee contributes 0 to every aggregate either way, so this only keeps the
// branch's row count proportional to orders that actually paid a delivery
// fee, not every order in scope.
const deliveryFeeFilter = " AND o.delivery_fees > 0"

// GetRevenueTotalsHT recomputes HT line-by-line from orders×orderitems×
// products×tva_categories. It CANNOT come from orders.ht/orders.tva: 100% of
// Uber Eats and Deliveroo orders have ht=0 there (PERIMETRE.md §1.5,
// wello-back-office repo — the two integrations never write those columns).
//
// This is a materially heavier query than GetRevenueTotalsTTC (a 4-table
// join with a per-line CASE and an aggregated LEFT JOIN, instead of a single
// table scan) — its cost on the PROD data volume is UNMEASURED from this
// environment (no staging access, no local Docker daemon available when
// this was written — see docs/analytics/MESURES.md's "Non mesuré" section).
// Callers must treat this as opt-in (RevenueRequest.IncludeHT) until that
// measurement exists, and be ready to disable it from the frontend alone if
// it turns out too expensive for this instance's fusible.
func (r *Repository) GetRevenueTotalsHT(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) (int64, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT `+roundToIntExpr("COALESCE(SUM("+htLineExpr+"), 0)")+`
	`) + "\n" + htLineJoins + "\nWHERE " + where

	var htCents int64
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, args...).Scan(&htCents)
	})
	if err != nil {
		return 0, fmt.Errorf("get revenue totals HT: %w", err)
	}
	return htCents, nil
}

// GetRevenueTimeline returns one row per (local day, channel) with the TTC
// sum, for the establishment's IANA timezone name (tzName, e.g.
// "Europe/Paris"). Only channels with at least one order that day are
// present — see RevenueDayPoint's doc comment for why that matters
// (AUDIT.md I4).
//
// tzName, not a fixed offset: a query spanning a DST transition (any 12-month
// window does) has rows on both sides of it. `AT TIME ZONE ?` bound to the
// zone name lets Postgres resolve each row's offset individually from its own
// creation_date, exactly like timeutil.LocalDayBounds does in Go — a single
// offset computed once from the period's start date (the previous
// implementation, via timeutil.TZOffset) is correct for that start date only;
// applied to every row it silently misattributes local-midnight-adjacent
// orders on the far side of the transition to the wrong calendar day. Do not
// revert to `?::interval` — that cast rejects a zone name outright, and a
// fixed offset reintroduces the bug even if it type-checks.
//
// tzName must be an IANA name, never a bare "+01:00"-style offset string:
// Postgres's text-zone overload of AT TIME ZONE treats a bare offset as POSIX
// TZ syntax, whose sign convention is the OPPOSITE of the interval-cast form
// this function used to use — passing "+01:00" here silently subtracts an
// hour instead of adding one. No error, no type mismatch, just a wrong
// answer.
func (r *Repository) GetRevenueTimeline(ctx context.Context, merchantIDs []string, tzName string, startUTC, endUTC time.Time) ([]RevenueDayPoint, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT to_char(o.creation_date AT TIME ZONE ?, 'YYYY-MM-DD') AS local_day,
			`+channelCaseExpr+` AS channel,
			COALESCE(SUM(o.price), 0) AS ttc_cents
		FROM orders o
	`) + "\nWHERE " + where + `
		GROUP BY local_day, channel
		ORDER BY local_day ASC
	`

	queryArgs := append([]interface{}{tzName}, args...)

	dayMap := make(map[string]*RevenueDayPoint)
	order := make([]string, 0)

	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, queryArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var localDay, channel string
			var ttcCents int64
			if err := rows.Scan(&localDay, &channel, &ttcCents); err != nil {
				return err
			}
			point, ok := dayMap[localDay]
			if !ok {
				point = &RevenueDayPoint{LocalDay: localDay, ByChannelTTCCents: map[string]int64{}}
				dayMap[localDay] = point
				order = append(order, localDay)
			}
			point.ByChannelTTCCents[channel] += ttcCents
			point.TotalTTCCents += ttcCents
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get revenue timeline: %w", err)
	}

	result := make([]RevenueDayPoint, 0, len(order))
	for _, day := range order {
		result = append(result, *dayMap[day])
	}
	return result, nil
}

// GetRevenueByChannel returns TTC totals for the whole period, one row per
// channel that had at least one order — the real computation AUDIT.md I8
// flagged as missing (the mock applied hardcoded coefficients to the total
// instead of grouping).
func (r *Repository) GetRevenueByChannel(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]RevenueChannelTotal, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT `+channelCaseExpr+` AS channel,
			COALESCE(SUM(o.price), 0) AS ttc_cents,
			COUNT(*) AS order_count
		FROM orders o
	`) + "\nWHERE " + where + `
		GROUP BY channel
		ORDER BY channel
	`

	result := make([]RevenueChannelTotal, 0, len(Channels))
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row RevenueChannelTotal
			if err := rows.Scan(&row.Channel, &row.TotalTTCCents, &row.OrderCount); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get revenue by channel: %w", err)
	}
	return result, nil
}

// GetRevenueByMerchant returns TTC totals grouped by merchant_id — used only
// when the request's group_by is "merchant". With today's single-merchant
// accessible scope this always returns exactly one row; the query itself is
// already correct for a wider scope once one exists.
func (r *Repository) GetRevenueByMerchant(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]RevenueMerchantTotal, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT o.merchant_id,
			COALESCE(SUM(o.price), 0) AS ttc_cents,
			COUNT(*) AS order_count
		FROM orders o
	`) + "\nWHERE " + where + `
		GROUP BY o.merchant_id
		ORDER BY o.merchant_id
	`

	result := make([]RevenueMerchantTotal, 0, len(merchantIDs))
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row RevenueMerchantTotal
			if err := rows.Scan(&row.MerchantID, &row.TotalTTCCents, &row.OrderCount); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get revenue by merchant: %w", err)
	}
	return result, nil
}

// ---- Commandes (POST /analytics/orders) ----

// OrdersTotals is the raw aggregate row behind OrdersPeriodTotals — covers
// come back as two counters (sum + how many orders actually carried a
// value), never a bare sum, so the service layer can tell "zero covers
// entered" from "covers genuinely zero" (places_settings is unset on 99.9%
// of PROD orders, PERIMETRE.md).
type OrdersTotals struct {
	OrderCount                 int64
	TotalTTCCents              int64
	TotalCovers                int64
	OrdersWithCovers           int64
	TTCCentsOfOrdersWithCovers int64
}

// GetOrdersTotals sums orders and TTC, and separately the covers-bearing
// subset — AvgBasketPerCoverCents is meant to divide "revenue of the orders
// that recorded covers" by "covers recorded", not the whole period's revenue
// by a partial covers count, which would understate the true per-cover
// basket whenever coverage is partial.
func (r *Repository) GetOrdersTotals(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) (OrdersTotals, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT COUNT(*),
			COALESCE(SUM(o.price), 0),
			COALESCE(SUM(CASE WHEN o.places_settings > 0 THEN o.places_settings ELSE 0 END), 0),
			COUNT(*) FILTER (WHERE o.places_settings > 0),
			COALESCE(SUM(CASE WHEN o.places_settings > 0 THEN o.price ELSE 0 END), 0)
		FROM orders o
	`) + "\nWHERE " + where

	var totals OrdersTotals
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, args...).Scan(
			&totals.OrderCount, &totals.TotalTTCCents,
			&totals.TotalCovers, &totals.OrdersWithCovers, &totals.TTCCentsOfOrdersWithCovers,
		)
	})
	if err != nil {
		return OrdersTotals{}, fmt.Errorf("get orders totals: %w", err)
	}
	return totals, nil
}

// GetOrdersTimeline mirrors GetRevenueTimeline (same tzName contract — see
// its doc comment for why a bare offset string must never be passed here)
// but counts orders instead of summing TTC.
func (r *Repository) GetOrdersTimeline(ctx context.Context, merchantIDs []string, tzName string, startUTC, endUTC time.Time) ([]OrdersDayPoint, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT to_char(o.creation_date AT TIME ZONE ?, 'YYYY-MM-DD') AS local_day,
			`+channelCaseExpr+` AS channel,
			COUNT(*) AS order_count
		FROM orders o
	`) + "\nWHERE " + where + `
		GROUP BY local_day, channel
		ORDER BY local_day ASC
	`

	queryArgs := append([]interface{}{tzName}, args...)

	dayMap := make(map[string]*OrdersDayPoint)
	order := make([]string, 0)

	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, queryArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var localDay, channel string
			var count int64
			if err := rows.Scan(&localDay, &channel, &count); err != nil {
				return err
			}
			point, ok := dayMap[localDay]
			if !ok {
				point = &OrdersDayPoint{LocalDay: localDay, ByChannelOrders: map[string]int64{}}
				dayMap[localDay] = point
				order = append(order, localDay)
			}
			point.ByChannelOrders[channel] += count
			point.TotalOrders += count
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get orders timeline: %w", err)
	}

	result := make([]OrdersDayPoint, 0, len(order))
	for _, day := range order {
		result = append(result, *dayMap[day])
	}
	return result, nil
}

// GetOrdersByChannel returns order counts for the whole period, one row per
// channel with at least one order.
func (r *Repository) GetOrdersByChannel(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]OrdersChannelTotal, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT `+channelCaseExpr+` AS channel,
			COUNT(*) AS order_count
		FROM orders o
	`) + "\nWHERE " + where + `
		GROUP BY channel
		ORDER BY channel
	`

	result := make([]OrdersChannelTotal, 0, len(Channels))
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row OrdersChannelTotal
			if err := rows.Scan(&row.Channel, &row.OrderCount); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get orders by channel: %w", err)
	}
	return result, nil
}

// ---- Règlements (POST /analytics/payments) ----

// paymentsScopeJoin is shared by every payments query: payments joined to
// orders under the canonical analytics scope, plus payments.enabled = TRUE —
// 1,142 PROD payment rows are disabled (P13, docs/analytics/AUDIT.md; 562 CB,
// 312 ES) and a SUM without this filter overstates encashment. The join is
// on orders so a payment tied to an order outside the canonical scope (a
// canceled/deleted order, a lowercase-brand_status row) is excluded exactly
// like every other analytics tab, not just orders directly.
func paymentsScopeJoin(merchantIDs []string, startUTC, endUTC time.Time) (string, []interface{}) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	return where + "\n\t\tAND p.enabled = TRUE", args
}

type PaymentsTotals struct {
	TotalAmountCents int64
	PaymentCount     int64
}

func (r *Repository) GetPaymentsTotals(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) (PaymentsTotals, error) {
	where, args := paymentsScopeJoin(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT COALESCE(SUM(p.amount), 0), COUNT(*)
		FROM payments p
		INNER JOIN orders o ON o.order_id = p.order_id
	`) + "\nWHERE " + where

	var totals PaymentsTotals
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, args...).Scan(&totals.TotalAmountCents, &totals.PaymentCount)
	})
	if err != nil {
		return PaymentsTotals{}, fmt.Errorf("get payments totals: %w", err)
	}
	return totals, nil
}

// GetPaymentsTimeline buckets by the order's local creation day (o.creation_date),
// not the payment's own payment_date — this keeps "period" single-sourced
// with every other analytics tab (a period request always means "orders
// created in this window"), rather than opening a second, payment-date-based
// notion of period that would disagree with the KPI totals above whenever an
// order's payment is recorded on a different calendar day than the order.
func (r *Repository) GetPaymentsTimeline(ctx context.Context, merchantIDs []string, tzName string, startUTC, endUTC time.Time) ([]PaymentsDayPoint, error) {
	where, args := paymentsScopeJoin(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT to_char(o.creation_date AT TIME ZONE ?, 'YYYY-MM-DD') AS local_day,
			`+paymentMethodCaseExpr+` AS method,
			COALESCE(SUM(p.amount), 0) AS amount_cents
		FROM payments p
		INNER JOIN orders o ON o.order_id = p.order_id
	`) + "\nWHERE " + where + `
		GROUP BY local_day, method
		ORDER BY local_day ASC
	`

	queryArgs := append([]interface{}{tzName}, args...)

	dayMap := make(map[string]*PaymentsDayPoint)
	order := make([]string, 0)

	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, queryArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var localDay, method string
			var amountCents int64
			if err := rows.Scan(&localDay, &method, &amountCents); err != nil {
				return err
			}
			point, ok := dayMap[localDay]
			if !ok {
				point = &PaymentsDayPoint{LocalDay: localDay, ByMethodAmountCents: map[string]int64{}}
				dayMap[localDay] = point
				order = append(order, localDay)
			}
			point.ByMethodAmountCents[method] += amountCents
			point.TotalAmountCents += amountCents
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get payments timeline: %w", err)
	}

	result := make([]PaymentsDayPoint, 0, len(order))
	for _, day := range order {
		result = append(result, *dayMap[day])
	}
	return result, nil
}

func (r *Repository) GetPaymentsByMethod(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]PaymentMethodTotal, error) {
	where, args := paymentsScopeJoin(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT `+paymentMethodCaseExpr+` AS method,
			COALESCE(SUM(p.amount), 0) AS amount_cents,
			COUNT(*) AS payment_count
		FROM payments p
		INNER JOIN orders o ON o.order_id = p.order_id
	`) + "\nWHERE " + where + `
		GROUP BY method
		ORDER BY method
	`

	result := make([]PaymentMethodTotal, 0, len(PaymentMethods))
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row PaymentMethodTotal
			if err := rows.Scan(&row.Method, &row.TotalAmountCents, &row.PaymentCount); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get payments by method: %w", err)
	}
	return result, nil
}

// ---- TVA (POST /analytics/vat) ----

type VATTotals struct {
	TotalTTCCents int64
	TotalHTCents  int64
}

// GetVATTotals reuses htLineExpr/htLineJoins (the same 4-table join
// GetRevenueTotalsHT uses) — TTC and HT come from the same line scan so
// TotalVATCents (service-computed as TTC-HT) never drifts from a
// separately-rounded VAT sum. UNION ALL'd with the delivery-fee branch
// (deliveryFeeHTExpr/deliveryFeeJoins — see their doc comment) so the total
// includes delivery fee VAT: a restaurateur checking VAT collected expects
// to see it (decision recorded in docs/decisions.md, PROMPT 09 lot 3, C5).
//
// Before this change this endpoint never read orders.delivery_fees at all —
// pos/reports did (its own UNION ALL branch, GetTVAReportData,
// internal/modules/pos/reports/repository.go), which was the single named
// component of the analytics-vs-pos/reports gap that pulled pos/reports'
// total UP rather than down (VATResponse's doc comment has the full,
// updated reconciliation). Verified read-only against staging (merchant
// 212, 12-month window, 2026-09-03): including this branch does not change
// the "0 orderitem lines reach tva_id=-1" fact from before this change —
// delivery fees still never appear in orderitems, they arrive exclusively
// through this second branch, on orders.delivery_fees.
func (r *Repository) GetVATTotals(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) (VATTotals, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT COALESCE(SUM(ttc_cents), 0) AS ttc_cents,
			`+roundToIntExpr("COALESCE(SUM(ht_raw), 0)")+` AS ht_cents
		FROM (
			SELECT (oi.price + COALESCE(e.extra_price, 0)) * oi.quantity AS ttc_cents,
				`+htLineExpr+` AS ht_raw
	`) + "\n\t\t" + strings.TrimSpace(htLineJoins) + `
			WHERE ` + where + `
			UNION ALL
			SELECT o.delivery_fees AS ttc_cents, ` + deliveryFeeHTExpr + ` AS ht_raw
	` + "\n\t\t" + strings.TrimSpace(deliveryFeeJoins) + `
			WHERE ` + where + deliveryFeeFilter + `
		) lines
	`

	allArgs := append(append([]interface{}{}, args...), args...)

	var totals VATTotals
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, allArgs...).Scan(&totals.TotalTTCCents, &totals.TotalHTCents)
	})
	if err != nil {
		return VATTotals{}, fmt.Errorf("get vat totals: %w", err)
	}
	return totals, nil
}

// VATRateShare is GetVATByRate's raw row: HTRaw is the group's unrounded HT
// sum, deliberately not rounded in SQL — see apportion.go's doc comment on
// why an independent per-group ROUND() cannot be trusted to reconcile with
// the period total, and service.go's apportionVATByRate for how the final,
// reconciling VATRateTotal.BaseHTCents is derived from it.
type VATRateShare struct {
	Rate     float64
	TTCCents int64
	HTRaw    float64
}

// GetVATByRate groups the same line scan by tva_categories.tva_rate,
// UNION ALL'd with the delivery-fee branch (see GetVATTotals) so a period
// with delivery fees gets a tva_id=-1 rate row (or is folded into an
// existing row at the same rate — grouped by rate value, not by
// tva_categories.tva_id, so a delivery fee at 20% lands in the same bucket
// as a product line taxed at 20%, which is the correct fiscal grouping: the
// tab groups "how much was taxed at this rate," not "which category".
func (r *Repository) GetVATByRate(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]VATRateShare, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT rate, COALESCE(SUM(ttc_cents), 0) AS ttc_cents, COALESCE(SUM(ht_raw), 0) AS ht_raw
		FROM (
			SELECT tva.tva_rate AS rate,
				(oi.price + COALESCE(e.extra_price, 0)) * oi.quantity AS ttc_cents,
				`+htLineExpr+` AS ht_raw
	`) + "\n\t\t" + strings.TrimSpace(htLineJoins) + `
			WHERE ` + where + `
			UNION ALL
			SELECT tva_fees.tva_rate AS rate, o.delivery_fees AS ttc_cents, ` + deliveryFeeHTExpr + ` AS ht_raw
	` + "\n\t\t" + strings.TrimSpace(deliveryFeeJoins) + `
			WHERE ` + where + deliveryFeeFilter + `
		) lines
		GROUP BY rate
		ORDER BY rate
	`

	allArgs := append(append([]interface{}{}, args...), args...)

	var result []VATRateShare
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, allArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row VATRateShare
			if err := rows.Scan(&row.Rate, &row.TTCCents, &row.HTRaw); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get vat by rate: %w", err)
	}
	return result, nil
}

// VATChannelShare is GetVATByChannel's raw row — see VATRateShare's doc
// comment.
type VATChannelShare struct {
	Channel  string
	TTCCents int64
	HTRaw    float64
}

// GetVATByChannel groups the same line scan by channelCaseExpr — the join
// keys the channel off orders (aliased `o` in htLineJoins), consistent with
// every other by-channel query in this package. UNION ALL'd with the
// delivery-fee branch (see GetVATTotals); channelCaseExpr only references
// o.brand/o.order_type, so it applies unchanged to the delivery-fee branch's
// plain `orders o` (no orderitems join needed there).
func (r *Repository) GetVATByChannel(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]VATChannelShare, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT channel, COALESCE(SUM(ttc_cents), 0) AS ttc_cents, COALESCE(SUM(ht_raw), 0) AS ht_raw
		FROM (
			SELECT `+channelCaseExpr+` AS channel,
				(oi.price + COALESCE(e.extra_price, 0)) * oi.quantity AS ttc_cents,
				`+htLineExpr+` AS ht_raw
	`) + "\n\t\t" + strings.TrimSpace(htLineJoins) + `
			WHERE ` + where + `
			UNION ALL
			SELECT ` + channelCaseExpr + ` AS channel, o.delivery_fees AS ttc_cents, ` + deliveryFeeHTExpr + ` AS ht_raw
	` + "\n\t\t" + strings.TrimSpace(deliveryFeeJoins) + `
			WHERE ` + where + deliveryFeeFilter + `
		) lines
		GROUP BY channel
		ORDER BY channel
	`

	allArgs := append(append([]interface{}{}, args...), args...)

	var result []VATChannelShare
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, allArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row VATChannelShare
			if err := rows.Scan(&row.Channel, &row.TTCCents, &row.HTRaw); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get vat by channel: %w", err)
	}
	return result, nil
}
