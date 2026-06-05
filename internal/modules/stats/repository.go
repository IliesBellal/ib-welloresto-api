package stats

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
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
	err := r.database.QueryRowContext(ctx, query, merchantID).Scan(&timezone)
	if err != nil {
		return "", fmt.Errorf("failed to get merchant timezone: %w", err)
	}

	return timezone, nil
}

// GetRevenue retrieves revenue data for specified date range (accounting for merchant timezone)
func (r *StatsRepository) GetRevenue(ctx context.Context, merchantID string, merchantTz *time.Location, dateInMerchantTz time.Time) (today, yesterday, weekCurrent, weekPrevious, monthCurrent, monthPrevious int64, err error) {
	// Today
	startToday := time.Date(dateInMerchantTz.Year(), dateInMerchantTz.Month(), dateInMerchantTz.Day(), 0, 0, 0, 0, merchantTz)
	endToday := startToday.Add(24 * time.Hour)

	// Convert to UTC for database query
	startTodayUTC := startToday.UTC()
	endTodayUTC := endToday.UTC()

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
	err := r.database.QueryRowContext(ctx, query, args...).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to get revenue for period: %w", err)
	}

	return total, nil
}

// getRevenueHTForPeriod sums HT for orders in a given period (expects UTC times).
func (r *StatsRepository) getRevenueHTForPeriod(ctx context.Context, merchantID string, startTimeUTC, endTimeUTC time.Time) (int64, error) {
	query, args := buildOrdersAggregateQuery("COALESCE(SUM(o.HT), 0) as total", merchantID, startTimeUTC, endTimeUTC)

	var total int64
	err := r.database.QueryRowContext(ctx, query, args...).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to get revenue HT for period: %w", err)
	}

	return total, nil
}

func (r *StatsRepository) ListRevenueHTByLocalDay(ctx context.Context, merchantID, tzOffset string, startTimeUTC, endTimeUTC time.Time) ([]RevenueHTByLocalDay, error) {
	whereClause := sharedOrdersRevenueWhereClause()
	query := strings.TrimSpace(`
		SELECT DATE_FORMAT(CONVERT_TZ(o.creation_date, '+00:00', ?), '%Y-%m-%d') AS local_day,
			COALESCE(SUM(o.HT), 0) AS revenue_ht_cents
		FROM orders o
	`) + "\n" + whereClause + `
		GROUP BY local_day
		ORDER BY local_day ASC
	`

	args := make([]interface{}, 0, 4)
	args = append(args, tzOffset)
	args = append(args, sharedOrdersRevenueWhereArgs(merchantID, startTimeUTC, endTimeUTC)...)

	rows, err := r.database.QueryContext(ctx, query, args...)
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
	startToday := time.Date(dateInMerchantTz.Year(), dateInMerchantTz.Month(), dateInMerchantTz.Day(), 0, 0, 0, 0, merchantTz)
	endToday := startToday.Add(24 * time.Hour)

	today, err = r.getOrderCountForPeriod(ctx, merchantID, startToday.UTC(), endToday.UTC())
	if err != nil {
		return 0, 0, err
	}

	// Yesterday
	startYesterday := startToday.Add(-24 * time.Hour)
	yesterday, err = r.getOrderCountForPeriod(ctx, merchantID, startYesterday.UTC(), startToday.UTC())
	if err != nil {
		return 0, 0, err
	}

	return
}

// getOrderCountForPeriod counts orders in a given period (expects UTC times)
func (r *StatsRepository) getOrderCountForPeriod(ctx context.Context, merchantID string, startTimeUTC, endTimeUTC time.Time) (int, error) {
	query, args := buildOrdersAggregateQuery("COUNT(*) as count", merchantID, startTimeUTC, endTimeUTC)

	var count int
	err := r.database.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get order count for period: %w", err)
	}

	return count, nil
}

// GetAverageBasket retrieves average basket size for today and yesterday (accounting for merchant timezone)
func (r *StatsRepository) GetAverageBasket(ctx context.Context, merchantID string, merchantTz *time.Location, dateInMerchantTz time.Time) (today, yesterday int64, err error) {
	// Today
	startToday := time.Date(dateInMerchantTz.Year(), dateInMerchantTz.Month(), dateInMerchantTz.Day(), 0, 0, 0, 0, merchantTz)
	endToday := startToday.Add(24 * time.Hour)

	today, err = r.getAverageBasketForPeriod(ctx, merchantID, startToday.UTC(), endToday.UTC())
	if err != nil {
		return 0, 0, err
	}

	// Yesterday
	startYesterday := startToday.Add(-24 * time.Hour)
	yesterday, err = r.getAverageBasketForPeriod(ctx, merchantID, startYesterday.UTC(), startToday.UTC())
	if err != nil {
		return 0, 0, err
	}

	return
}

// getAverageBasketForPeriod calculates average basket size for a period (expects UTC times)
func (r *StatsRepository) getAverageBasketForPeriod(ctx context.Context, merchantID string, startTimeUTC, endTimeUTC time.Time) (int64, error) {
	query, args := buildOrdersAggregateQuery("ROUND(COALESCE(AVG(o.price), 0),0) as avg_basket", merchantID, startTimeUTC, endTimeUTC)

	var avgBasket int64
	err := r.database.QueryRowContext(ctx, query, args...).Scan(&avgBasket)
	if err != nil {
		return 0, fmt.Errorf("failed to get average basket for period: %w", err)
	}

	return avgBasket, nil
}

// GetHourlyData retrieves hourly breakdown of orders for today (accounting for merchant timezone)
// Returns two separate datasets: revenue and order counts
func (r *StatsRepository) GetHourlyData(ctx context.Context, merchantID string, merchantTz *time.Location, dateInMerchantTz time.Time) (hourlyRevenue, hourlyOrders []map[string]interface{}, err error) {
	startDay := time.Date(dateInMerchantTz.Year(), dateInMerchantTz.Month(), dateInMerchantTz.Day(), 0, 0, 0, 0, merchantTz)
	endDay := startDay.Add(24 * time.Hour)

	// Convert to UTC for database query
	startDayUTC := startDay.UTC()
	endDayUTC := endDay.UTC()

	query := `
	SELECT 
		HOUR(CONVERT_TZ(o.creation_date, '+00:00', ?)) as hour,
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
	AND o.isPaid = 1
	GROUP BY HOUR(CONVERT_TZ(o.creation_date, '+00:00', ?))
	ORDER BY hour
	`

	// Convert timezone to UTC offset format (+HH:MM)
	tzOffset := GetTZOffset(merchantTz, dateInMerchantTz)

	rows, err := r.database.QueryContext(ctx, query, tzOffset, merchantID, startDayUTC, endDayUTC, tzOffset)
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

// getTZOffset converts a time.Location to UTC offset format (+HH:MM or -HH:MM)
// Required for MySQL CONVERT_TZ function
func GetTZOffset(tz *time.Location, t time.Time) string {
	_, offset := t.In(tz).Zone()
	hours := offset / 3600
	minutes := (offset % 3600) / 60

	if offset >= 0 {
		return fmt.Sprintf("+%02d:%02d", hours, minutes)
	}
	return fmt.Sprintf("%03d:%02d", hours, minutes)
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
