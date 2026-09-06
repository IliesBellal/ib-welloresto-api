package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/permission"
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

// ResolveAccessibleMerchants returns every establishment where user holds
// permission.POSAnalytics via an active users_rights link — the accessible
// scope for the whole multi-establishment analytics page (PROMPT 23). This
// supersedes the PROMPT 03 version of this function (a bare
// []string{user.MerchantID}, guarded by a doc comment forbidding exactly
// this query as "a scope-widening regression") — that guard held only until
// multi-establishment access became an explicit product decision (PROMPT
// 23's "Décisions arrêtées" table: "la permission définit elle-même la
// portée"), which it now is.
//
// ONE query, not one per users_rights link — PROMPT 23 §1's explicit
// requirement, and not optional: this runs on every analytics request,
// against an instance sized at 0.1 vCPU and shared with the POS.
//
// Resolves both RBAC worlds with the SAME query, matching UserLoginRow.Has
// (auth/permissions.go) exactly for each link it scans:
//   - role_id IS NOT NULL: the link's role must carry permission.POSAnalytics
//     in role_permissions. No system_key='admin' special-case — RBAC lot 11
//     removed that short-circuit from Has() itself, so an admin role only
//     grants this because seed_system_roles / the full-catalog invariant
//     (TestSystemAdminRolesContainFullCatalog_Postgres) guarantees it
//     actually carries every catalog key, pos.analytics included.
//   - role_id IS NULL: the historical world. pos.analytics has no entry in
//     auth.legacyPermissionFallback — Has()'s only fallback for a key absent
//     from that map is Rights.Admin. So a role_id-nil link is accessible
//     here iff users_rights.admin = true, exactly mirroring Has().
//
// Excludes user_id = '' EXPLICITLY ("AND ur.user_id <> ''"), not just
// incidentally via "WHERE ur.user_id = $1": staging carries 4 users_rights
// rows sharing user_id = '' across 4 unrelated establishments (173, 2, 2,
// 203 — orphaned rows with no matching `users` row, see
// docs/analytics/DROITS.md §1.1/§2.1, wello-back-office repo). The one
// scenario where that would matter — a UserLoginRow whose own UserID is
// itself "" — is exactly what this guard makes structurally impossible:
// "ur.user_id <> ''" rejects every one of those 4 rows regardless of what
// userID holds, so an empty UserID can never merge 4 unrelated
// establishments' access into one caller's scope. See
// postgres_integration_scope_test.go for the test that proves it.
//
// Only "enabled AND login_enabled" links count: a disabled or
// login-disabled link must not widen what an active session can read, even
// though it can no longer be used to authenticate on its own.
//
// Does NOT special-case the token's own merchant being absent from the
// result: if that link itself doesn't carry pos.analytics, the caller would
// not see the Analyse page at all (pos.analytics also gates the frontend
// menu entry — DROITS.md §6.2), so an accessible scope that excludes it is
// the correct, fail-closed answer, not a bug. ValidateRequestedMerchants
// then rejects any merchant_id outside this (possibly empty) result with a
// 403, never a silent narrowing.
func (r *Repository) ResolveAccessibleMerchants(ctx context.Context, user *auth.UserLoginRow) ([]string, error) {
	if user == nil || strings.TrimSpace(user.UserID) == "" {
		return nil, nil
	}

	rows, err := dbx.GetDB(ctx, r.db).QueryContext(ctx, `
		SELECT DISTINCT ur.merchant_id
		FROM users_rights ur
		LEFT JOIN role_permissions rp
			ON rp.role_id = ur.role_id AND rp.permission_key = ?
		WHERE ur.user_id = ?
		  AND ur.user_id <> ''
		  AND ur.enabled = TRUE
		  AND ur.login_enabled = TRUE
		  AND (
		    (ur.role_id IS NOT NULL AND rp.role_id IS NOT NULL)
		    OR (ur.role_id IS NULL AND ur.admin = TRUE)
		  )
	`, string(permission.POSAnalytics), user.UserID)
	if err != nil {
		return nil, fmt.Errorf("resolve accessible merchants: %w", err)
	}
	defer rows.Close()

	var merchantIDs []string
	for rows.Next() {
		var merchantID string
		if err := rows.Scan(&merchantID); err != nil {
			return nil, fmt.Errorf("scan accessible merchant: %w", err)
		}
		merchantIDs = append(merchantIDs, merchantID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate accessible merchants: %w", err)
	}
	return merchantIDs, nil
}

// GetMerchantNames resolves display names for a set of merchant IDs — used
// only to label a scope already resolved by ResolveAccessibleMerchants
// (PROMPT 24 Phase 1), never to determine access itself. Returns an entry per
// row actually found; a merchantID with no matching `merchant` row (should
// not happen for an ID that just came out of users_rights, but this is a
// display path, not a security check) is silently omitted rather than erroring.
func (r *Repository) GetMerchantNames(ctx context.Context, merchantIDs []string) ([]AccessibleMerchant, error) {
	if len(merchantIDs) == 0 {
		return nil, nil
	}

	// merchant.id is an integer identity while merchantIDs is carried as
	// strings everywhere in Go (see auth.authMerchantJoinCast's doc comment,
	// 12-merchant-id-unification.md) — this package already assumes the
	// Postgres-only dialect elsewhere (the `= ANY(?)` array form throughout
	// scope.go), so CAST(... AS TEXT) is written directly, no dialect switch.
	rows, err := dbx.GetDB(ctx, r.db).QueryContext(ctx, `
		SELECT id, fullName
		FROM merchant
		WHERE CAST(id AS TEXT) = ANY(?)
		ORDER BY fullName
	`, merchantIDs)
	if err != nil {
		return nil, fmt.Errorf("get merchant names: %w", err)
	}
	defer rows.Close()

	var merchants []AccessibleMerchant
	for rows.Next() {
		var m AccessibleMerchant
		if err := rows.Scan(&m.MerchantID, &m.Name); err != nil {
			return nil, fmt.Errorf("scan merchant name: %w", err)
		}
		merchants = append(merchants, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate merchant names: %w", err)
	}
	return merchants, nil
}

// HasForMerchant reports whether a user holds permission key on a SPECIFIC
// establishment — the cross-establishment counterpart to UserLoginRow.Has,
// which only ever answers for the token's own merchant (its own doc comment:
// "sur son établissement courant"). PROMPT 23 needs this to gate the merged
// nominative blocks (per-server rankings): merging them across establishments
// is only safe when the caller holds the block's permission on EVERY
// selected establishment, not just the one they are logged into.
//
// MUST return the exact same verdict as user.Has(key) when merchantID equals
// user's own token merchant — see the postgres integration test
// TestHasForMerchant_AgreesWithHasOnTokenMerchant, which exists specifically
// because PROMPT 23 flags that two independent implementations of the same
// rule eventually diverge, and a test is how that gets caught instead of
// discovered in production.
//
// One query: looks up the (user_id, merchant_id) users_rights link
// (enabled AND login_enabled — an inactive link grants nothing, same rule
// ResolveAccessibleMerchants applies) and, in the same round trip, whether
// its role (if any) carries key. Then in Go:
//   - role_id IS NOT NULL: the joined role_permissions match decides it,
//     no system_key short-circuit — identical reasoning to
//     ResolveAccessibleMerchants above.
//   - role_id IS NULL: Rights.Admin short-circuits to true, else
//     auth.LegacyFallback(key, rights) — the exact historical-world rule
//     Has() applies, reused via that accessor rather than reimplemented, so
//     there is nothing here to keep in sync by hand. This package never
//     touches auth.legacyPermissionFallback itself (PROMPT 23's explicit
//     instruction).
//   - no matching active link at all: false, never an error — "not linked
//     there" and "linked but lacking the permission" are the same
//     caller-facing answer.
func (r *Repository) HasForMerchant(ctx context.Context, userID, merchantID string, key permission.Key) (bool, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(merchantID) == "" {
		return false, nil
	}

	var roleID sql.NullString
	var roleGranted bool
	var rights auth.UserRowRights
	err := dbx.GetDB(ctx, r.db).QueryRowContext(ctx, `
		SELECT ur.role_id,
		       (ur.role_id IS NOT NULL AND rp.role_id IS NOT NULL),
		       ur.admin,
		       ur.access_wrreception, ur.print_merchant_cash_report, ur.open_cash_drawer,
		       ur.manage_menu, ur.manage_plannings, ur.manage_users, ur.manage_settings, ur.manage_haccp,
		       ur.view_reports, ur.view_financials, ur.manage_customers
		FROM users_rights ur
		LEFT JOIN role_permissions rp
			ON rp.role_id = ur.role_id AND rp.permission_key = ?
		WHERE ur.user_id = ?
		  AND ur.user_id <> ''
		  AND ur.merchant_id = ?
		  AND ur.enabled = TRUE
		  AND ur.login_enabled = TRUE
		LIMIT 1
	`, string(key), userID, merchantID).Scan(
		&roleID, &roleGranted, &rights.Admin,
		&rights.AccessReception, &rights.PrintMerchantCashReport, &rights.OpenCashDrawer,
		&rights.CanManageMenu, &rights.CanManagePlannings, &rights.CanManageUsers, &rights.CanManageSettings, &rights.CanManageHACCP,
		&rights.CanViewReports, &rights.CanViewFinancials, &rights.CanManageCustomers,
	)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has for merchant: %w", err)
	}

	if roleID.Valid {
		return roleGranted, nil
	}
	if rights.Admin {
		return true, nil
	}
	granted, _ := auth.LegacyFallback(key, rights)
	return granted, nil
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

// GetOrdersByMerchant returns order counts and TTC totals grouped by
// merchant_id — PROMPT 23 Phase 3: group_by=merchant was accepted by
// OrdersRequest and echoed back in Scope.GroupBy since PROMPT 03 (the same
// contract as GetRevenueByMerchant's), but nothing ever computed this
// breakdown — a dead parameter, worse than an absent one, since a caller had
// no way to tell "ignored" from "there happened to be nothing to break
// down". Mirrors GetRevenueByMerchant exactly: COUNT/SUM on integer columns,
// grouped by merchant_id, so — unlike a derived HT figure — group totals
// summed back together always equal the ungrouped total exactly, no
// apportionment needed.
func (r *Repository) GetOrdersByMerchant(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]OrdersMerchantTotal, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT o.merchant_id,
			COUNT(*) AS order_count,
			COALESCE(SUM(o.price), 0) AS ttc_cents
		FROM orders o
	`) + "\nWHERE " + where + `
		GROUP BY o.merchant_id
		ORDER BY o.merchant_id
	`

	result := make([]OrdersMerchantTotal, 0, len(merchantIDs))
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row OrdersMerchantTotal
			if err := rows.Scan(&row.MerchantID, &row.OrderCount, &row.TotalTTCCents); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get orders by merchant: %w", err)
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

// GetPaymentsByMerchant returns amount/count totals grouped by merchant_id —
// PROMPT 24 Phase 2. Mirrors GetRevenueByMerchant/GetOrdersByMerchant
// exactly: COUNT/SUM on integer columns, so rows sum back to the ungrouped
// total exactly, no apportionment needed.
func (r *Repository) GetPaymentsByMerchant(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]PaymentsMerchantTotal, error) {
	where, args := paymentsScopeJoin(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT o.merchant_id,
			COALESCE(SUM(p.amount), 0) AS amount_cents,
			COUNT(*) AS payment_count
		FROM payments p
		INNER JOIN orders o ON o.order_id = p.order_id
	`) + "\nWHERE " + where + `
		GROUP BY o.merchant_id
		ORDER BY o.merchant_id
	`

	result := make([]PaymentsMerchantTotal, 0, len(merchantIDs))
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row PaymentsMerchantTotal
			if err := rows.Scan(&row.MerchantID, &row.TotalAmountCents, &row.PaymentCount); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get payments by merchant: %w", err)
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

// ---- TVA — group_by=merchant (PROMPT 24 Phase 2) ----
//
// The three functions below mirror GetVATTotals/GetVATByRate/GetVATByChannel
// exactly, adding o.merchant_id to the SELECT list and GROUP BY — same
// UNION ALL of htLineJoins (product lines) and deliveryFeeJoins (delivery
// fees), both aliasing orders as `o`. Only Repository.GetVATTotalsByMerchant's
// TotalHTCents is rounded in SQL (roundToIntExpr, one ROUND per
// establishment); GetVATByRateByMerchant/GetVATByChannelByMerchant return the
// raw (unrounded) per-establishment HT shares, exactly like their
// non-grouped counterparts, so service.go can apportion each establishment's
// shares against THAT establishment's own rounded total (see
// buildVATByMerchant) — never against the combined scope's total, which
// would leave an establishment's own parts not summing to its own total.

// VATMerchantTotals is GetVATTotalsByMerchant's raw per-establishment row.
type VATMerchantTotals struct {
	MerchantID    string
	TotalTTCCents int64
	TotalHTCents  int64
}

func (r *Repository) GetVATTotalsByMerchant(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]VATMerchantTotals, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT merchant_id,
			COALESCE(SUM(ttc_cents), 0) AS ttc_cents,
			`+roundToIntExpr("COALESCE(SUM(ht_raw), 0)")+` AS ht_cents
		FROM (
			SELECT o.merchant_id AS merchant_id,
				(oi.price + COALESCE(e.extra_price, 0)) * oi.quantity AS ttc_cents,
				`+htLineExpr+` AS ht_raw
	`) + "\n\t\t" + strings.TrimSpace(htLineJoins) + `
			WHERE ` + where + `
			UNION ALL
			SELECT o.merchant_id AS merchant_id, o.delivery_fees AS ttc_cents, ` + deliveryFeeHTExpr + ` AS ht_raw
	` + "\n\t\t" + strings.TrimSpace(deliveryFeeJoins) + `
			WHERE ` + where + deliveryFeeFilter + `
		) lines
		GROUP BY merchant_id
		ORDER BY merchant_id
	`

	allArgs := append(append([]interface{}{}, args...), args...)

	result := make([]VATMerchantTotals, 0, len(merchantIDs))
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, allArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row VATMerchantTotals
			if err := rows.Scan(&row.MerchantID, &row.TotalTTCCents, &row.TotalHTCents); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get vat totals by merchant: %w", err)
	}
	return result, nil
}

// VATRateShareByMerchant is GetVATByRateByMerchant's raw row — see
// VATRateShare's doc comment for why HTRaw stays unrounded here.
type VATRateShareByMerchant struct {
	MerchantID string
	Rate       float64
	TTCCents   int64
	HTRaw      float64
}

func (r *Repository) GetVATByRateByMerchant(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]VATRateShareByMerchant, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT merchant_id, rate, COALESCE(SUM(ttc_cents), 0) AS ttc_cents, COALESCE(SUM(ht_raw), 0) AS ht_raw
		FROM (
			SELECT o.merchant_id AS merchant_id, tva.tva_rate AS rate,
				(oi.price + COALESCE(e.extra_price, 0)) * oi.quantity AS ttc_cents,
				`+htLineExpr+` AS ht_raw
	`) + "\n\t\t" + strings.TrimSpace(htLineJoins) + `
			WHERE ` + where + `
			UNION ALL
			SELECT o.merchant_id AS merchant_id, tva_fees.tva_rate AS rate, o.delivery_fees AS ttc_cents, ` + deliveryFeeHTExpr + ` AS ht_raw
	` + "\n\t\t" + strings.TrimSpace(deliveryFeeJoins) + `
			WHERE ` + where + deliveryFeeFilter + `
		) lines
		GROUP BY merchant_id, rate
		ORDER BY merchant_id, rate
	`

	allArgs := append(append([]interface{}{}, args...), args...)

	var result []VATRateShareByMerchant
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, allArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row VATRateShareByMerchant
			if err := rows.Scan(&row.MerchantID, &row.Rate, &row.TTCCents, &row.HTRaw); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get vat by rate by merchant: %w", err)
	}
	return result, nil
}

// VATChannelShareByMerchant is GetVATByChannelByMerchant's raw row — see
// VATChannelShare's doc comment for why HTRaw stays unrounded here.
type VATChannelShareByMerchant struct {
	MerchantID string
	Channel    string
	TTCCents   int64
	HTRaw      float64
}

func (r *Repository) GetVATByChannelByMerchant(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]VATChannelShareByMerchant, error) {
	where, args := AnalyticsOrdersScope(merchantIDs, startUTC, endUTC)
	query := strings.TrimSpace(`
		SELECT merchant_id, channel, COALESCE(SUM(ttc_cents), 0) AS ttc_cents, COALESCE(SUM(ht_raw), 0) AS ht_raw
		FROM (
			SELECT o.merchant_id AS merchant_id, `+channelCaseExpr+` AS channel,
				(oi.price + COALESCE(e.extra_price, 0)) * oi.quantity AS ttc_cents,
				`+htLineExpr+` AS ht_raw
	`) + "\n\t\t" + strings.TrimSpace(htLineJoins) + `
			WHERE ` + where + `
			UNION ALL
			SELECT o.merchant_id AS merchant_id, ` + channelCaseExpr + ` AS channel, o.delivery_fees AS ttc_cents, ` + deliveryFeeHTExpr + ` AS ht_raw
	` + "\n\t\t" + strings.TrimSpace(deliveryFeeJoins) + `
			WHERE ` + where + deliveryFeeFilter + `
		) lines
		GROUP BY merchant_id, channel
		ORDER BY merchant_id, channel
	`

	allArgs := append(append([]interface{}{}, args...), args...)

	var result []VATChannelShareByMerchant
	err := r.runTx(ctx, func(ctx context.Context, tx *dbx.DB) error {
		rows, err := tx.QueryContext(ctx, query, allArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var row VATChannelShareByMerchant
			if err := rows.Scan(&row.MerchantID, &row.Channel, &row.TTCCents, &row.HTRaw); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get vat by channel by merchant: %w", err)
	}
	return result, nil
}
