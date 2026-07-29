package performance

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	statspkg "welloresto-api/internal/modules/stats"
)

type StatsReader interface {
	GetMerchantTimezone(ctx context.Context, merchantID string) (string, error)
	ListRevenueHTByLocalDay(ctx context.Context, merchantID, tzOffset string, startTimeUTC, endTimeUTC time.Time) ([]statspkg.RevenueHTByLocalDay, error)
}

type Repository struct {
	db    *sql.DB
	stats StatsReader
}

// plnDayFmt formate une date/timestamp en 'YYYY-MM-DD' selon le dialecte.
func plnDayFmt(expr string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "to_char(" + expr + ", 'YYYY-MM-DD')"
	}
	return "DATE_FORMAT(" + expr + ", '%Y-%m-%d')"
}

func NewRepository(db *sql.DB, stats StatsReader) *Repository {
	return &Repository{db: db, stats: stats}
}

func (r *Repository) ListPlannedByDayEmployee(ctx context.Context, merchantID string, fromLocalDay, toLocalDay time.Time) ([]PlannedByDayEmployeeRow, error) {
	fromDay := normalizeDateOnlyUTC(fromLocalDay)
	toDay := normalizeDateOnlyUTC(toLocalDay)
	if toDay.Before(fromDay) {
		return []PlannedByDayEmployeeRow{}, nil
	}

	db := dbx.GetDB(ctx, r.db)
	// TIMESTAMP(date, time)/TIMESTAMPDIFF/DATE_ADD sont MySQL-only : la branche
	// PG additionne date + time (types natifs) et convertit l'écart via EPOCH.
	query := `
		SELECT DATE_FORMAT(s.shift_date, '%Y-%m-%d') AS local_day,
			s.employee_id,
			SUM(
				GREATEST(
					0,
					(
						CASE
							WHEN s.end_time >= s.start_time THEN TIMESTAMPDIFF(
								MINUTE,
								TIMESTAMP(s.shift_date, s.start_time),
								TIMESTAMP(s.shift_date, s.end_time)
							)
							ELSE TIMESTAMPDIFF(
								MINUTE,
								TIMESTAMP(s.shift_date, s.start_time),
								DATE_ADD(TIMESTAMP(s.shift_date, s.end_time), INTERVAL 1 DAY)
							)
						END
					) - COALESCE(s.break_minutes, 0)
				)
			) AS planned_minutes
		FROM planning_shifts s
		WHERE s.merchant_id = ?
			AND s.enabled = TRUE
			AND s.status <> 'cancelled'
			AND s.employee_id IS NOT NULL
			AND s.shift_date >= ?
			AND s.shift_date <= ?
		GROUP BY local_day, s.employee_id
		ORDER BY local_day ASC, s.employee_id ASC
	`
	if dbx.ActiveDialect() == dbx.Postgres {
		query = `
		SELECT to_char(s.shift_date, 'YYYY-MM-DD') AS local_day,
			s.employee_id,
			SUM(
				GREATEST(
					0,
					(
						CASE
							WHEN s.end_time >= s.start_time THEN
								CAST(FLOOR(EXTRACT(EPOCH FROM ((s.shift_date + s.end_time) - (s.shift_date + s.start_time))) / 60) AS integer)
							ELSE
								CAST(FLOOR(EXTRACT(EPOCH FROM ((s.shift_date + s.end_time + INTERVAL '1' DAY) - (s.shift_date + s.start_time))) / 60) AS integer)
						END
					) - COALESCE(s.break_minutes, 0)
				)
			) AS planned_minutes
		FROM planning_shifts s
		WHERE s.merchant_id = ?
			AND s.enabled = TRUE
			AND s.shift_date >= ?
			AND s.shift_date <= ?
			AND s.employee_id IS NOT NULL
		GROUP BY local_day, s.employee_id
		ORDER BY local_day ASC, s.employee_id ASC
	`
	}

	rows, err := db.QueryContext(ctx, query, merchantID, fromDay.Format("2006-01-02"), toDay.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("list planned by day employee: %w", err)
	}
	defer rows.Close()

	items := make([]PlannedByDayEmployeeRow, 0)
	for rows.Next() {
		var row PlannedByDayEmployeeRow
		if err := rows.Scan(&row.LocalDay, &row.EmployeeID, &row.PlannedMinutes); err != nil {
			return nil, fmt.Errorf("scan planned by day employee: %w", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate planned by day employee: %w", err)
	}

	return items, nil
}

func (r *Repository) ListWorkedRawByDayEmployee(ctx context.Context, merchantID string, fromLocalDay, toLocalDay time.Time) ([]WorkedRawByDayEmployeeRow, error) {
	_, tzOffset, startUTC, endUTC, err := r.resolveMerchantRangeBounds(ctx, merchantID, fromLocalDay, toLocalDay)
	if err != nil {
		return nil, err
	}
	if !endUTC.After(startUTC) {
		return []WorkedRawByDayEmployeeRow{}, nil
	}

	db := dbx.GetDB(ctx, r.db)
	// CONVERT_TZ -> AT TIME ZONE avec cast INTERVAL obligatoire (écart n°1 du
	// rapport 25) ; TIMESTAMPDIFF(SECOND, ...) -> EXTRACT(EPOCH ...).
	localDayExpr := `DATE_FORMAT(CONVERT_TZ(te.clock_in_at, '+00:00', ?), '%Y-%m-%d')`
	workedExpr := `TIMESTAMPDIFF(SECOND, te.clock_in_at, te.clock_out_at)`
	if dbx.ActiveDialect() == dbx.Postgres {
		localDayExpr = `to_char(te.clock_in_at AT TIME ZONE (?::interval), 'YYYY-MM-DD')`
		workedExpr = `CAST(FLOOR(EXTRACT(EPOCH FROM (te.clock_out_at - te.clock_in_at))) AS bigint)`
	}
	query := `
		SELECT ` + localDayExpr + ` AS local_day,
			te.employee_id,
			SUM(GREATEST(0, ` + workedExpr + `)) AS worked_seconds
		FROM planning_time_entries te
		WHERE te.merchant_id = ?
			AND te.enabled = TRUE
			AND te.clock_out_at IS NOT NULL
			AND te.clock_in_at >= ?
			AND te.clock_in_at < ?
		GROUP BY local_day, te.employee_id
		ORDER BY local_day ASC, te.employee_id ASC
	`

	rows, err := db.QueryContext(ctx, query, tzOffset, merchantID, startUTC, endUTC)
	if err != nil {
		return nil, fmt.Errorf("list worked raw by day employee: %w", err)
	}
	defer rows.Close()

	items := make([]WorkedRawByDayEmployeeRow, 0)
	for rows.Next() {
		var row WorkedRawByDayEmployeeRow
		if err := rows.Scan(&row.LocalDay, &row.EmployeeID, &row.WorkedSeconds); err != nil {
			return nil, fmt.Errorf("scan worked raw by day employee: %w", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate worked raw by day employee: %w", err)
	}

	return items, nil
}

func (r *Repository) ListRevenueByDay(ctx context.Context, merchantID string, fromLocalDay, toLocalDay time.Time) ([]RevenueByDayRow, error) {
	_, tzOffset, startUTC, endUTC, err := r.resolveMerchantRangeBounds(ctx, merchantID, fromLocalDay, toLocalDay)
	if err != nil {
		return nil, err
	}
	if !endUTC.After(startUTC) {
		return []RevenueByDayRow{}, nil
	}

	rows, err := r.stats.ListRevenueHTByLocalDay(ctx, merchantID, tzOffset, startUTC, endUTC)
	if err != nil {
		return nil, fmt.Errorf("list revenue by day: %w", err)
	}

	items := make([]RevenueByDayRow, 0, len(rows))
	for _, row := range rows {
		items = append(items, RevenueByDayRow{LocalDay: row.LocalDay, RevenueHTCents: row.RevenueHTCents})
	}
	return items, nil
}

type RevenueForecastByDayRow struct {
	LocalDay      string
	AmountHTCents int64
}

func (r *Repository) ListRevenueForecastByDay(ctx context.Context, merchantID string, fromLocalDay, toLocalDay time.Time) ([]RevenueForecastByDayRow, error) {
	fromDay := normalizeDateOnlyUTC(fromLocalDay)
	toDay := normalizeDateOnlyUTC(toLocalDay)
	if toDay.Before(fromDay) {
		return []RevenueForecastByDayRow{}, nil
	}

	db := dbx.GetDB(ctx, r.db)
	query := `
		SELECT ` + plnDayFmt("forecast_date") + ` AS local_day, amount_ht_cents
		FROM planning_revenue_forecasts
		WHERE merchant_id = ? AND forecast_date >= ? AND forecast_date <= ?
		ORDER BY local_day ASC
	`

	rows, err := db.QueryContext(ctx, query, merchantID, fromDay.Format("2006-01-02"), toDay.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("list revenue forecast by day: %w", err)
	}
	defer rows.Close()

	items := make([]RevenueForecastByDayRow, 0)
	for rows.Next() {
		var row RevenueForecastByDayRow
		if err := rows.Scan(&row.LocalDay, &row.AmountHTCents); err != nil {
			return nil, fmt.Errorf("scan revenue forecast by day: %w", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate revenue forecast by day: %w", err)
	}

	return items, nil
}

func (r *Repository) ListRatesByEmployee(ctx context.Context, merchantID string) ([]EmployeeRateRow, error) {
	db := dbx.GetDB(ctx, r.db)
	query := `
		SELECT e.id, e.hourly_rate, e.employer_charges_pct
		FROM employees e
		WHERE e.merchant_id = ?
			AND e.enabled = TRUE
		ORDER BY e.id ASC
	`

	rows, err := db.QueryContext(ctx, query, strings.TrimSpace(merchantID))
	if err != nil {
		return nil, fmt.Errorf("list rates by employee: %w", err)
	}
	defer rows.Close()

	items := make([]EmployeeRateRow, 0)
	for rows.Next() {
		var row EmployeeRateRow
		if err := rows.Scan(&row.EmployeeID, &row.HourlyRateCents, &row.EmployerChargesPct); err != nil {
			return nil, fmt.Errorf("scan rates by employee: %w", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rates by employee: %w", err)
	}

	return items, nil
}

func (r *Repository) resolveMerchantRangeBounds(ctx context.Context, merchantID string, fromLocalDay, toLocalDay time.Time) (*time.Location, string, time.Time, time.Time, error) {
	timezone, err := r.stats.GetMerchantTimezone(ctx, merchantID)
	if err != nil {
		return nil, "", time.Time{}, time.Time{}, fmt.Errorf("resolve merchant timezone: %w", err)
	}

	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return nil, "", time.Time{}, time.Time{}, fmt.Errorf("load merchant timezone: %w", err)
	}

	fromDate := normalizeDateOnlyInLocation(fromLocalDay, location)
	toDate := normalizeDateOnlyInLocation(toLocalDay, location)
	if toDate.Before(fromDate) {
		return location, statspkg.GetTZOffset(location, fromDate), time.Time{}, time.Time{}, nil
	}

	startLocal := fromDate
	endExclusiveLocal := toDate.Add(24 * time.Hour)
	tzOffset := statspkg.GetTZOffset(location, startLocal)

	return location, tzOffset, startLocal.UTC(), endExclusiveLocal.UTC(), nil
}

func normalizeDateOnlyUTC(value time.Time) time.Time {
	utc := value.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func normalizeDateOnlyInLocation(value time.Time, location *time.Location) time.Time {
	local := value.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
}
