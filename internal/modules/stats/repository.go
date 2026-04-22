package stats

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// StatsRepository handles data access for stats module
type StatsRepository struct {
	database *sql.DB
}

// NewStatsRepository creates a new instance of StatsRepository
func NewStatsRepository(db *sql.DB) *StatsRepository {
	return &StatsRepository{database: db}
}

// GetRevenue retrieves revenue data for specified date range
func (r *StatsRepository) GetRevenue(ctx context.Context, merchantID string, date time.Time) (today, yesterday, weekCurrent, weekPrevious, monthCurrent, monthPrevious int64, err error) {
	// Today
	startToday := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	endToday := startToday.Add(24 * time.Hour)

	today, err = r.getRevenueForPeriod(ctx, merchantID, startToday, endToday)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	// Yesterday
	startYesterday := startToday.Add(-24 * time.Hour)
	yesterday, err = r.getRevenueForPeriod(ctx, merchantID, startYesterday, startToday)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	// This week (Monday to current day)
	currentWeekStart := getWeekStart(date)
	weekCurrent, err = r.getRevenueForPeriod(ctx, merchantID, currentWeekStart, endToday)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	// Previous week (same length as current week)
	prevWeekStart := currentWeekStart.Add(-7 * 24 * time.Hour)
	prevWeekEnd := currentWeekStart
	weekPrevious, err = r.getRevenueForPeriod(ctx, merchantID, prevWeekStart, prevWeekEnd)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	// This month
	startMonth := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	monthCurrent, err = r.getRevenueForPeriod(ctx, merchantID, startMonth, endToday)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	// Previous month
	prevMonth := startMonth.Add(-24 * time.Hour)
	prevMonthStart := time.Date(prevMonth.Year(), prevMonth.Month(), 1, 0, 0, 0, 0, date.Location())
	prevMonthEnd := startMonth
	monthPrevious, err = r.getRevenueForPeriod(ctx, merchantID, prevMonthStart, prevMonthEnd)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	return
}

// getRevenueForPeriod sums TTC (total price) for orders in a given period
func (r *StatsRepository) getRevenueForPeriod(ctx context.Context, merchantID string, startTime, endTime time.Time) (int64, error) {
	query := `
	SELECT COALESCE(SUM(o.price), 0) as total
	FROM orders o
	WHERE o.merchant_id = ?
	AND o.creation_date >= ?
	AND o.creation_date < ?
	AND o.state IN ('CLOSED', 'DONE')
	AND o.isPaid = 1
	`

	var total int64
	err := r.database.QueryRowContext(ctx, query, merchantID, startTime, endTime).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to get revenue for period: %w", err)
	}

	return total, nil
}

// GetOrderCount retrieves order count for today and yesterday
func (r *StatsRepository) GetOrderCount(ctx context.Context, merchantID string, date time.Time) (today, yesterday int, err error) {
	// Today
	startToday := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	endToday := startToday.Add(24 * time.Hour)

	today, err = r.getOrderCountForPeriod(ctx, merchantID, startToday, endToday)
	if err != nil {
		return 0, 0, err
	}

	// Yesterday
	startYesterday := startToday.Add(-24 * time.Hour)
	yesterday, err = r.getOrderCountForPeriod(ctx, merchantID, startYesterday, startToday)
	if err != nil {
		return 0, 0, err
	}

	return
}

// getOrderCountForPeriod counts orders in a given period
func (r *StatsRepository) getOrderCountForPeriod(ctx context.Context, merchantID string, startTime, endTime time.Time) (int, error) {
	query := `
	SELECT COUNT(*) as count
	FROM orders o
	WHERE o.merchant_id = ?
	AND o.creation_date >= ?
	AND o.creation_date < ?
	AND o.state IN ('CLOSED', 'DONE')
	`

	var count int
	err := r.database.QueryRowContext(ctx, query, merchantID, startTime, endTime).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get order count for period: %w", err)
	}

	return count, nil
}

// GetAverageBasket retrieves average basket size for today and yesterday
func (r *StatsRepository) GetAverageBasket(ctx context.Context, merchantID string, date time.Time) (today, yesterday int64, err error) {
	// Today
	startToday := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	endToday := startToday.Add(24 * time.Hour)

	today, err = r.getAverageBasketForPeriod(ctx, merchantID, startToday, endToday)
	if err != nil {
		return 0, 0, err
	}

	// Yesterday
	startYesterday := startToday.Add(-24 * time.Hour)
	yesterday, err = r.getAverageBasketForPeriod(ctx, merchantID, startYesterday, startToday)
	if err != nil {
		return 0, 0, err
	}

	return
}

// getAverageBasketForPeriod calculates average basket size for a period
func (r *StatsRepository) getAverageBasketForPeriod(ctx context.Context, merchantID string, startTime, endTime time.Time) (int64, error) {
	query := `
	SELECT COALESCE(AVG(o.price), 0) as avg_basket
	FROM orders o
	WHERE o.merchant_id = ?
	AND o.creation_date >= ?
	AND o.creation_date < ?
	AND o.state IN ('CLOSED', 'DONE')
	AND o.isPaid = 1
	`

	var avgBasket int64
	err := r.database.QueryRowContext(ctx, query, merchantID, startTime, endTime).Scan(&avgBasket)
	if err != nil {
		return 0, fmt.Errorf("failed to get average basket for period: %w", err)
	}

	return avgBasket, nil
}

// GetHourlyData retrieves hourly breakdown of orders for today
func (r *StatsRepository) GetHourlyData(ctx context.Context, merchantID string, date time.Time) ([]map[string]interface{}, error) {
	startDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	endDay := startDay.Add(24 * time.Hour)

	query := `
	SELECT 
		HOUR(o.creation_date) as hour,
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
	AND o.isPaid = 1
	GROUP BY HOUR(o.creation_date)
	ORDER BY hour
	`

	rows, err := r.database.QueryContext(ctx, query, merchantID, startDay, endDay)
	if err != nil {
		return nil, fmt.Errorf("failed to get hourly data: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var hour int
		var surPlaceCount, emporterCount, livraisonCount, uberEatsCount, deliverooCount int
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
			return nil, fmt.Errorf("failed to scan hourly data: %w", err)
		}

		results = append(results, map[string]interface{}{
			"hour":      hour,
			"sur_place": surPlaceCount,
			"emporter":  emporterCount,
			"livraison": livraisonCount,
			"uber_eats": uberEatsCount,
			"deliveroo": deliverooCount,
			"total":     surPlaceCount + emporterCount + livraisonCount + uberEatsCount + deliverooCount,
		})
	}

	return results, nil
}

// Helper function to get the start of the week (Monday)
func getWeekStart(date time.Time) time.Time {
	weekday := date.Weekday()
	// Weekday: Sunday=0, Monday=1, ..., Saturday=6
	// We want Monday=0, so we adjust
	daysToMonday := int(weekday) - 1
	if daysToMonday < 0 {
		daysToMonday = 6
	}
	return time.Date(date.Year(), date.Month(), date.Day()-daysToMonday, 0, 0, 0, 0, date.Location())
}
