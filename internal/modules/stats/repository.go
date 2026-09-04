package stats

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/timeutil"
)

// StatsRepository handles data access for stats module
type StatsRepository struct {
	database *sql.DB
}

type RevenueHTByLocalDay struct {
	LocalDay       string
	RevenueHTCents int64
}

// NewStatsRepository creates a new instance of StatsRepository
func NewStatsRepository(db *sql.DB) *StatsRepository {
	return &StatsRepository{database: db}
}

// GetMerchantTimezone retrieves the timezone for a specific merchant
func (r *StatsRepository) GetMerchantTimezone(ctx context.Context, merchantID string) (string, error) {
	query := `SELECT timezone FROM merchant WHERE id = ?`

	var timezone string
	err := dbx.GetDB(ctx, r.database).QueryRowContext(ctx, query, merchantID).Scan(&timezone)
	if err != nil {
		return "", fmt.Errorf("failed to get merchant timezone: %w", err)
	}

	return timezone, nil
}

// GetRevenue retrieves revenue data for specified date range (accounting for merchant timezone)
func (r *StatsRepository) GetRevenue(ctx context.Context, merchantID string, merchantTz *time.Location, dateInMerchantTz time.Time) (today, yesterday, weekCurrent, weekPrevious, monthCurrent, monthPrevious int64, err error) {
	// Today
	startTodayUTC, endTodayUTC := timeutil.LocalDayBounds(dateInMerchantTz, merchantTz)
	startToday := startTodayUTC.In(merchantTz)

	today, err = r.getRevenueForPeriod(ctx, merchantID, startTodayUTC, endTodayUTC)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	// Yesterday
	startYesterday := startToday.Add(-24 * time.Hour)
	endYesterday := startToday
	yesterday, err = r.getRevenueForPeriod(ctx, merchantID, startYesterday.UTC(), endYesterday.UTC())
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	// This week (Monday to current day)
	currentWeekStart := getWeekStart(dateInMerchantTz, merchantTz)
	weekCurrent, err = r.getRevenueForPeriod(ctx, merchantID, currentWeekStart.UTC(), endTodayUTC)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	// Previous week (same length as current week)
	prevWeekStart := currentWeekStart.Add(-7 * 24 * time.Hour)
	prevWeekEnd := currentWeekStart
	weekPrevious, err = r.getRevenueForPeriod(ctx, merchantID, prevWeekStart.UTC(), prevWeekEnd.UTC())
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	// This month
	startMonth := time.Date(dateInMerchantTz.Year(), dateInMerchantTz.Month(), 1, 0, 0, 0, 0, merchantTz)
	monthCurrent, err = r.getRevenueForPeriod(ctx, merchantID, startMonth.UTC(), endTodayUTC)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	// Previous month
	prevMonth := startMonth.Add(-24 * time.Hour)
	prevMonthStart := time.Date(prevMonth.Year(), prevMonth.Month(), 1, 0, 0, 0, 0, merchantTz)
	prevMonthEnd := startMonth
	monthPrevious, err = r.getRevenueForPeriod(ctx, merchantID, prevMonthStart.UTC(), prevMonthEnd.UTC())
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	return
}

// getRevenueForPeriod sums TTC (total price) for orders in a given period (expects UTC times)
func (r *StatsRepository) getRevenueForPeriod(ctx context.Context, merchantID string, startTimeUTC, endTimeUTC time.Time) (int64, error) {
	query, args := buildOrdersAggregateQuery("COALESCE(SUM(o.price), 0) as total", merchantID, startTimeUTC, endTimeUTC)

	var total int64
	err := dbx.GetDB(ctx, r.database).QueryRowContext(ctx, query, args...).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to get revenue for period: %w", err)
	}

	return total, nil
}

// getRevenueHTForPeriod sums HT for orders in a given period (expects UTC times).
func (r *StatsRepository) getRevenueHTForPeriod(ctx context.Context, merchantID string, startTimeUTC, endTimeUTC time.Time) (int64, error) {
	query, args := buildOrdersAggregateQuery("COALESCE(SUM(o.HT), 0) as total", merchantID, startTimeUTC, endTimeUTC)

	var total int64
	err := dbx.GetDB(ctx, r.database).QueryRowContext(ctx, query, args...).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to get revenue HT for period: %w", err)
	}

	return total, nil
}

func (r *StatsRepository) ListRevenueHTByLocalDay(ctx context.Context, merchantID, tzOffset string, startTimeUTC, endTimeUTC time.Time) ([]RevenueHTByLocalDay, error) {
	whereClause := sharedOrdersRevenueWhereClause()
	// CONVERT_TZ(x, '+00:00', tzOffset) has no direct Postgres equivalent.
	// AT TIME ZONE with a *text* offset follows the POSIX sign convention
	// (inverted vs. tzOffset's ISO sign) — casting the offset to INTERVAL
	// first makes AT TIME ZONE add it with the expected (ISO) sign, matching
	// CONVERT_TZ, and returns a naive timestamp immune to session tz reinterpretation.
	localDayExpr := "DATE_FORMAT(CONVERT_TZ(o.creation_date, '+00:00', ?), '%Y-%m-%d')"
	if dbx.ActiveDialect() == dbx.Postgres {
		localDayExpr = "to_char(o.creation_date AT TIME ZONE (?::interval), 'YYYY-MM-DD')"
	}
	query := strings.TrimSpace(`
		SELECT `+localDayExpr+` AS local_day,
			COALESCE(SUM(o.HT), 0) AS revenue_ht_cents
		FROM orders o
	`) + "\n" + whereClause + `
		GROUP BY local_day
		ORDER BY local_day ASC
	`

	args := make([]interface{}, 0, 4)
	args = append(args, tzOffset)
	args = append(args, sharedOrdersRevenueWhereArgs(merchantID, startTimeUTC, endTimeUTC)...)

	rows, err := dbx.GetDB(ctx, r.database).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list HT revenue by local day: %w", err)
	}
	defer rows.Close()

	items := make([]RevenueHTByLocalDay, 0)
	for rows.Next() {
		var row RevenueHTByLocalDay
		if err := rows.Scan(&row.LocalDay, &row.RevenueHTCents); err != nil {
			return nil, fmt.Errorf("failed to scan HT revenue row: %w", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate HT revenue rows: %w", err)
	}

	return items, nil
}

// GetOrderCount retrieves order count for today and yesterday (accounting for merchant timezone)
func (r *StatsRepository) GetOrderCount(ctx context.Context, merchantID string, merchantTz *time.Location, dateInMerchantTz time.Time) (today, yesterday int, err error) {
	// Today
	startTodayUTC, endTodayUTC := timeutil.LocalDayBounds(dateInMerchantTz, merchantTz)

	today, err = r.getOrderCountForPeriod(ctx, merchantID, startTodayUTC, endTodayUTC)
	if err != nil {
		return 0, 0, err
	}

	// Yesterday
	startYesterdayUTC := startTodayUTC.Add(-24 * time.Hour)
	yesterday, err = r.getOrderCountForPeriod(ctx, merchantID, startYesterdayUTC, startTodayUTC)
	if err != nil {
		return 0, 0, err
	}

	return
}

// getOrderCountForPeriod counts orders in a given period (expects UTC times)
func (r *StatsRepository) getOrderCountForPeriod(ctx context.Context, merchantID string, startTimeUTC, endTimeUTC time.Time) (int, error) {
	query, args := buildOrdersAggregateQuery("COUNT(*) as count", merchantID, startTimeUTC, endTimeUTC)

	var count int
	err := dbx.GetDB(ctx, r.database).QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get order count for period: %w", err)
	}

	return count, nil
}

// GetAverageBasket retrieves average basket size for today and yesterday (accounting for merchant timezone)
func (r *StatsRepository) GetAverageBasket(ctx context.Context, merchantID string, merchantTz *time.Location, dateInMerchantTz time.Time) (today, yesterday int64, err error) {
	// Today
	startTodayUTC, endTodayUTC := timeutil.LocalDayBounds(dateInMerchantTz, merchantTz)

	today, err = r.getAverageBasketForPeriod(ctx, merchantID, startTodayUTC, endTodayUTC)
	if err != nil {
		return 0, 0, err
	}

	// Yesterday
	startYesterdayUTC := startTodayUTC.Add(-24 * time.Hour)
	yesterday, err = r.getAverageBasketForPeriod(ctx, merchantID, startYesterdayUTC, startTodayUTC)
	if err != nil {
		return 0, 0, err
	}

	return
}

// getAverageBasketForPeriod calculates average basket size for a period (expects UTC times)
func (r *StatsRepository) getAverageBasketForPeriod(ctx context.Context, merchantID string, startTimeUTC, endTimeUTC time.Time) (int64, error) {
	query, args := buildOrdersAggregateQuery("ROUND(COALESCE(AVG(o.price), 0),0) as avg_basket", merchantID, startTimeUTC, endTimeUTC)

	var avgBasket int64
	err := dbx.GetDB(ctx, r.database).QueryRowContext(ctx, query, args...).Scan(&avgBasket)
	if err != nil {
		return 0, fmt.Errorf("failed to get average basket for period: %w", err)
	}

	return avgBasket, nil
}

// GetHourlyData retrieves hourly breakdown of orders for today (accounting for merchant timezone)
// Returns two separate datasets: revenue and order counts
func (r *StatsRepository) GetHourlyData(ctx context.Context, merchantID string, merchantTz *time.Location, dateInMerchantTz time.Time) (hourlyRevenue, hourlyOrders []map[string]interface{}, err error) {
	startDayUTC, endDayUTC := timeutil.LocalDayBounds(dateInMerchantTz, merchantTz)

	// Same CONVERT_TZ translation as ListRevenueHTByLocalDay: cast the offset
	// to INTERVAL so AT TIME ZONE adds it with the expected (ISO) sign.
	hourExpr := "HOUR(CONVERT_TZ(o.creation_date, '+00:00', ?))"
	if dbx.ActiveDialect() == dbx.Postgres {
		hourExpr = "EXTRACT(HOUR FROM (o.creation_date AT TIME ZONE (?::interval)))::int"
	}
	query := `
	SELECT
		` + hourExpr + ` as hour,
		SUM(CASE WHEN o.order_type = 'IN' AND (o.brand IS NULL OR o.brand = 'WELLO_RESTO') THEN o.price ELSE 0 END) as sur_place_revenue,
		COUNT(DISTINCT CASE WHEN o.order_type = 'IN' AND (o.brand IS NULL OR o.brand = 'WELLO_RESTO') THEN o.order_id END) as sur_place_count,
		SUM(CASE WHEN o.order_type = 'TAKE_AWAY' AND (o.brand IS NULL OR o.brand = 'WELLO_RESTO') THEN o.price ELSE 0 END) as emporter_revenue,
		COUNT(DISTINCT CASE WHEN o.order_type = 'TAKE_AWAY' AND (o.brand IS NULL OR o.brand = 'WELLO_RESTO') THEN o.order_id END) as emporter_count,
		SUM(CASE WHEN o.order_type = 'DELIVERY' AND (o.brand IS NULL OR o.brand = 'WELLO_RESTO') THEN o.price ELSE 0 END) as livraison_revenue,
		COUNT(DISTINCT CASE WHEN o.order_type = 'DELIVERY' AND (o.brand IS NULL OR o.brand = 'WELLO_RESTO') THEN o.order_id END) as livraison_count,
		SUM(CASE WHEN o.brand = 'UBER_EATS' THEN o.price ELSE 0 END) as uber_eats_revenue,
		COUNT(DISTINCT CASE WHEN o.brand = 'UBER_EATS' THEN o.order_id END) as uber_eats_count,
		SUM(CASE WHEN o.brand = 'DELIVEROO' THEN o.price ELSE 0 END) as deliveroo_revenue,
		COUNT(DISTINCT CASE WHEN o.brand = 'DELIVEROO' THEN o.order_id END) as deliveroo_count
	FROM orders o
	WHERE o.merchant_id = ?
	AND o.creation_date >= ?
	AND o.creation_date < ?
	AND o.state IN ('CLOSED', 'DONE')
	AND o.brand_status NOT IN ('DELETED', 'CANCELED')
	AND o.isPaid = true
	GROUP BY hour
	ORDER BY hour
	`

	// Convert timezone to UTC offset format (+HH:MM)
	tzOffset := GetTZOffset(merchantTz, dateInMerchantTz)

	rows, err := dbx.GetDB(ctx, r.database).QueryContext(ctx, query, tzOffset, merchantID, startDayUTC, endDayUTC)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get hourly data: %w", err)
	}
	defer rows.Close()

	revenueData := make([]map[string]interface{}, 0)
	orderData := make([]map[string]interface{}, 0)

	for rows.Next() {
		var hour int
		var surPlaceCount, emporterCount, livraisonCount, uberEatsCount, deliverooCount int64
		var surPlaceRevenue, emporterRevenue, livraisonRevenue, uberEatsRevenue, deliverooRevenue int64

		err := rows.Scan(
			&hour,
			&surPlaceRevenue, &surPlaceCount,
			&emporterRevenue, &emporterCount,
			&livraisonRevenue, &livraisonCount,
			&uberEatsRevenue, &uberEatsCount,
			&deliverooRevenue, &deliverooCount,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to scan hourly data: %w", err)
		}

		// Revenue data
		revenueData = append(revenueData, map[string]interface{}{
			"hour":      hour,
			"sur_place": surPlaceRevenue,
			"emporter":  emporterRevenue,
			"livraison": livraisonRevenue,
			"uber_eats": uberEatsRevenue,
			"deliveroo": deliverooRevenue,
			"total":     surPlaceRevenue + emporterRevenue + livraisonRevenue + uberEatsRevenue + deliverooRevenue,
		})

		// Order counts data
		orderData = append(orderData, map[string]interface{}{
			"hour":      hour,
			"sur_place": surPlaceCount,
			"emporter":  emporterCount,
			"livraison": livraisonCount,
			"uber_eats": uberEatsCount,
			"deliveroo": deliverooCount,
			"total":     surPlaceCount + emporterCount + livraisonCount + uberEatsCount + deliverooCount,
		})
	}

	return revenueData, orderData, nil
}

// UpsellTotals contient les totaux globaux upsell pour une période
type UpsellTotals struct {
	TotalLines     int64
	RevenueHTCents int64
}

// UpsellServerRow contient le détail upsell d'un serveur pour une période
type UpsellServerRow struct {
	ServerID        string
	ServerName      string
	UpsellLines     int64
	UpsellRevenueHT int64
}

// upsellLineHTExpr calcule le HT par ligne de commande à partir du TTC et du taux de TVA
// du produit, comme dans GetTVAReportData (mêmes jointures produits/extra/tva_categories).
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
	AND o.merchant_id = ?
	AND o.creation_date >= ?
	AND o.creation_date < ?
	AND o.state IN ('CLOSED', 'DONE')
	AND o.brand_status NOT IN ('DELETED', 'CANCELED')
`

const upsellLinesBaseQuery = upsellLinesFromJoins + upsellLinesWhereClause

// roundToIntExpr wraps a fractional SQL expression (upsellLineHTExpr divides
// by 100+tva_rate, which is rarely a whole number of cents) so it scans
// cleanly into an int64. ROUND(x, 0) accepts any numeric type in MySQL, but
// Postgres's two-argument ROUND only accepts numeric, not double precision —
// the tva_rate column (real) forces float arithmetic, so an explicit cast is
// required on the Postgres side.
func roundToIntExpr(expr string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "ROUND(CAST(" + expr + " AS numeric), 0)"
	}
	return "ROUND(" + expr + ", 0)"
}

// GetUpsellTotals retourne le nombre de lignes upsell et leur CA HT pour la période
func (r *StatsRepository) GetUpsellTotals(ctx context.Context, merchantID string, startTimeUTC, endTimeUTC time.Time) (UpsellTotals, error) {
	query := strings.TrimSpace(`
		SELECT COUNT(*) AS total_lines, `+roundToIntExpr("COALESCE(SUM("+upsellLineHTExpr+"), 0)")+` AS revenue_ht
	`) + "\n" + upsellLinesBaseQuery

	var totals UpsellTotals
	err := dbx.GetDB(ctx, r.database).QueryRowContext(ctx, query, merchantID, startTimeUTC, endTimeUTC).Scan(&totals.TotalLines, &totals.RevenueHTCents)
	if err != nil {
		return UpsellTotals{}, fmt.Errorf("failed to get upsell totals: %w", err)
	}

	return totals, nil
}

// GetOrdersWithUpsellCount retourne le nombre de commandes distinctes ayant au moins une ligne upsell sur la période
func (r *StatsRepository) GetOrdersWithUpsellCount(ctx context.Context, merchantID string, startTimeUTC, endTimeUTC time.Time) (int, error) {
	query := strings.TrimSpace(`
		SELECT COUNT(DISTINCT o.order_id)
	`) + "\n" + upsellLinesBaseQuery

	var count int
	err := dbx.GetDB(ctx, r.database).QueryRowContext(ctx, query, merchantID, startTimeUTC, endTimeUTC).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get orders with upsell count: %w", err)
	}

	return count, nil
}

// ListUpsellByServer retourne le classement des serveurs par CA upsell HT décroissant.
// Les commandes self-service (SCANNORDER / sans utilisateur) sont exclues : un serveur
// ne peut pas être crédité d'une suggestion qu'il n'a pas portée.
func (r *StatsRepository) ListUpsellByServer(ctx context.Context, merchantID string, startTimeUTC, endTimeUTC time.Time) ([]UpsellServerRow, error) {
	query := strings.TrimSpace(`
		SELECT o.created_by AS server_id,
			COALESCE(TRIM(CONCAT(COALESCE(u.first_name, ''), ' ', COALESCE(u.last_name, ''))), o.created_by) AS server_name,
			COUNT(*) AS upsell_lines,
			`+roundToIntExpr("COALESCE(SUM("+upsellLineHTExpr+"), 0)")+` AS upsell_revenue_ht
	`) + "\n" + upsellLinesFromJoins + `
		LEFT JOIN users u ON u.user_id = o.created_by
	` + upsellLinesWhereClause + `
		AND o.created_by NOT IN ('-1', 'SCANNORDER')
		GROUP BY o.created_by, server_name
		ORDER BY upsell_revenue_ht DESC
	`

	rows, err := dbx.GetDB(ctx, r.database).QueryContext(ctx, query, merchantID, startTimeUTC, endTimeUTC)
	if err != nil {
		return nil, fmt.Errorf("failed to list upsell by server: %w", err)
	}
	defer rows.Close()

	items := make([]UpsellServerRow, 0)
	for rows.Next() {
		var row UpsellServerRow
		if err := rows.Scan(&row.ServerID, &row.ServerName, &row.UpsellLines, &row.UpsellRevenueHT); err != nil {
			return nil, fmt.Errorf("failed to scan upsell by server row: %w", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate upsell by server rows: %w", err)
	}

	return items, nil
}

// ============ HELPER FUNCTIONS ============

// getWeekStart returns the start of the week (Monday) in the given timezone
func getWeekStart(dateInTz time.Time, tz *time.Location) time.Time {
	weekday := dateInTz.Weekday()
	// Weekday: Sunday=0, Monday=1, ..., Saturday=6
	// We want Monday, so we calculate days back
	daysToMonday := int(weekday) - 1
	if daysToMonday < 0 {
		daysToMonday = 6
	}
	return time.Date(dateInTz.Year(), dateInTz.Month(), dateInTz.Day()-daysToMonday, 0, 0, 0, 0, tz)
}

// GetTZOffset converts a time.Location to UTC offset format (+HH:MM or -HH:MM).
// Thin alias kept for existing callers (planning/performance, this module's
// own tests) — the implementation now lives in internal/timeutil so
// internal/modules/analytics can use it without importing stats.
func GetTZOffset(tz *time.Location, t time.Time) string {
	return timeutil.TZOffset(tz, t)
}

func buildOrdersAggregateQuery(selectExpr, merchantID string, startTimeUTC, endTimeUTC time.Time) (string, []interface{}) {
	query := strings.TrimSpace(`
		SELECT `+selectExpr+`
		FROM orders o
	`) + "\n" + sharedOrdersRevenueWhereClause()

	args := sharedOrdersRevenueWhereArgs(merchantID, startTimeUTC, endTimeUTC)
	return query, args
}

func sharedOrdersRevenueWhereClause() string {
	return strings.TrimSpace(`
		WHERE o.merchant_id = ?
		AND o.creation_date >= ?
		AND o.creation_date < ?
		AND o.state IN ('CLOSED', 'DONE')
		AND o.brand_status NOT IN ('DELETED', 'CANCELED')
	`)
}

func sharedOrdersRevenueWhereArgs(merchantID string, startTimeUTC, endTimeUTC time.Time) []interface{} {
	return []interface{}{merchantID, startTimeUTC, endTimeUTC}
}
