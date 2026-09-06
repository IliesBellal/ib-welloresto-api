package analytics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
)

// ---- Clients (POST /analytics/clients, POST /analytics/clients/top) ----
//
// PROMPT 18. Two endpoints, same split as Annulations (PROMPT 10, see
// cancellations.go's package-level doc comment): an aggregate view (new
// customers, recurring rate, segments, frequency — permission.
// ReportsSalesRead, same door as every other tab) and a nominative ranking
// (name, lifetime value, last visit, avg basket — permission.CustomersManage,
// is_sensitive, whose catalog label already covers "consultation et export
// des fiches clients" — no new permission key needed).
//
// Three rules this file's queries obey, all named explicitly in PROMPT 18 §3:
//
//   - customer.customer_nb_orders / customer.customer_total_spent /
//     customer.last_order_date are NEVER read here: verified wrong for 29%/
//     50% of customers on the PROD perimeter (the brief's own audit figure).
//     They are also all-time, WELLO_RESTO-only counters (only that brand's
//     order-validation path increments them — see internal/modules/customers/
//     repository.go's ValidateOrder), so they could never answer a
//     channel-filtered, period-scoped question even if they were accurate.
//     Every count/sum/date in this file is recomputed directly from `orders`,
//     through this package's existing AnalyticsOrdersScope, same posture as
//     every other tab.
//   - "Nouveau client" is whether the customer's FIRST ORDER EVER — not
//     bounded to the requested period — falls inside [periodStart,
//     periodEnd). Restricting MIN(creation_date) to the period itself would
//     make every active customer "new," more severely the shorter the
//     window (PROMPT 18 §3's own example).
//   - customer.creation_date is an import date, not a first-order date — it
//     is never read here for anything.
//
// To make the first two possible without running two incompatible queries,
// GetCustomersLifetimeStats computes every per-customer aggregate over the
// customer's WHOLE order history up to periodEnd (exclusive) — never bounded
// by periodStart — using periodEnd (the report's own horizon) as the upper
// bound, not wall-clock now(). This keeps a report for a past period
// reproducible: re-running it later for the same dates returns the same
// numbers, instead of drifting as more orders accrue after the fact.
// "Cumulée depuis toujours" is read as "since the start of this
// establishment's history, up to this report's own end date" — see
// customersEpoch below.
const customersEpoch = "2000-01-01"

// customerDisplayNameExpr mirrors cancellations.go's GetCancellationsByStaff
// display-name fallback chain (first+last, then a single stored name, then a
// synthetic label so a row is never blank) — `c` is aliased to `customer`,
// `pc` to the per_customer CTE built around it in this file's queries.
const customerDisplayNameExpr = `
	COALESCE(
		NULLIF(TRIM(CONCAT(COALESCE(c.customer_first_name, ''), ' ', COALESCE(c.customer_last_name, ''))), ''),
		NULLIF(c.customer_name, ''),
		'Client ' || pc.customer_id::text
	)
`

// CustomersCoverage is GetCustomersCoverage's row — the tab's mandatory
// coverage disclosure (PROMPT 18 §1): the analysis only ever covers orders
// carrying a customer_id, and that share must stay visible next to every
// number this tab prints, whatever channel filter is applied — a screen
// silent on this point would read as "these are our best customers" instead
// of "these are the 14% of orders we could identify."
type CustomersCoverage struct {
	OrdersWithCustomer int64
	TotalOrders        int64
}

// GetCustomersCoverage counts, within [periodStart, periodEnd) and the
// requested channel filter, how many orders carry a customer_id versus the
// period total — independent of GetCustomersLifetimeStats below, which only
// ever sees the covered (customer_id IS NOT NULL) subset.
func (r *Repository) GetCustomersCoverage(ctx context.Context, merchantIDs []string, channels []string, startUTC, endUTC time.Time) (CustomersCoverage, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT
			COUNT(*) FILTER (WHERE o.customer_id IS NOT NULL),
			COUNT(*)
		FROM orders o
	`) + "\nWHERE " + where + ` AND (` + channelCaseExpr + `) = ANY(?)`
	args = append(args, channels)

	var cov CustomersCoverage
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		return tx.QueryRowContext(ctx, query, args...).Scan(&cov.OrdersWithCustomer, &cov.TotalOrders)
	})
	if err != nil {
		return CustomersCoverage{}, fmt.Errorf("get customers coverage: %w", err)
	}
	return cov, nil
}

// CustomerLifetimeRow is one identified customer's full profile as of
// periodEnd — see this file's doc comment for why every field except
// PeriodOrders/PeriodRevenueCents is computed over the customer's whole
// history rather than just the requested period.
type CustomerLifetimeRow struct {
	CustomerID         string
	DisplayName        string
	FirstOrderDate     time.Time
	LastOrderDate      time.Time
	LifetimeOrders     int64
	LifetimeValueCents int64
	PeriodOrders       int64
	PeriodRevenueCents int64
}

// GetCustomersLifetimeStats returns one row per customer_id with at least one
// qualifying order (AnalyticsOrdersScope: state CLOSED/DONE, brand_status not
// deleted/canceled) for this merchant and channel filter, from customersEpoch
// up to periodEnd — every customer this establishment has ever identified,
// not just those active in the requested period, since both segmentation
// (nouveau/récurrent/fidèle/inactif) and the recurring-rate/materiality
// checks need the full picture at once, not a page of it.
//
// This is necessarily a full scan of the establishment's order history,
// bounded by merchant_id: the existing idx_orders_merchant_creation
// (merchant_id, creation_date) index still applies here (merchant_id equality
// + creation_date < periodEnd is exactly that index's shape), so no new index
// is introduced for this tab. PROMPT 18 §3 accepts the extra cost of
// recomputing from orders explicitly ("c'est plus coûteux et c'est la seule
// façon d'être juste"), and its Vérification section asks for no timing
// measurement on this tab.
func (r *Repository) GetCustomersLifetimeStats(ctx context.Context, merchantIDs []string, channels []string, periodStartUTC, periodEndUTC time.Time) ([]CustomerLifetimeRow, error) {
	epoch, err := time.Parse("2006-01-02", customersEpoch)
	if err != nil {
		return nil, fmt.Errorf("parse customers epoch: %w", err)
	}

	scopeWhere, scopeArgs := AnalyticsOrdersScope(merchantIDs, epoch, periodEndUTC)

	query := "WITH scoped AS (\n" +
		"\tSELECT o.customer_id, o.creation_date, o.price\n" +
		"\tFROM orders o\n" +
		"\tWHERE " + scopeWhere + " AND o.customer_id IS NOT NULL AND (" + channelCaseExpr + ") = ANY(?)\n" +
		"),\n" +
		"per_customer AS (\n" +
		"\tSELECT customer_id,\n" +
		"\t\tMIN(creation_date) AS first_order_date,\n" +
		"\t\tMAX(creation_date) AS last_order_date,\n" +
		"\t\tCOUNT(*) AS lifetime_orders,\n" +
		"\t\tCOALESCE(SUM(price), 0) AS lifetime_value_cents,\n" +
		"\t\tCOUNT(*) FILTER (WHERE creation_date >= ? AND creation_date < ?) AS period_orders,\n" +
		"\t\tCOALESCE(SUM(price) FILTER (WHERE creation_date >= ? AND creation_date < ?), 0) AS period_revenue_cents\n" +
		"\tFROM scoped\n" +
		"\tGROUP BY customer_id\n" +
		")\n" +
		"SELECT pc.customer_id::text, " + customerDisplayNameExpr + ",\n" +
		"\tpc.first_order_date, pc.last_order_date, pc.lifetime_orders, pc.lifetime_value_cents,\n" +
		"\tpc.period_orders, pc.period_revenue_cents\n" +
		"FROM per_customer pc\n" +
		"LEFT JOIN customer c ON c.customer_id = pc.customer_id\n"

	args := append([]interface{}{}, scopeArgs...)
	args = append(args, channels)
	args = append(args, periodStartUTC, periodEndUTC, periodStartUTC, periodEndUTC)

	var result []CustomerLifetimeRow
	err = r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row CustomerLifetimeRow
			if err := rows.Scan(
				&row.CustomerID, &row.DisplayName,
				&row.FirstOrderDate, &row.LastOrderDate, &row.LifetimeOrders, &row.LifetimeValueCents,
				&row.PeriodOrders, &row.PeriodRevenueCents,
			); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get customers lifetime stats: %w", err)
	}
	return result, nil
}
