package performance

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"
	settingspkg "welloresto-api/internal/modules/planning/settings"
	sharedpkg "welloresto-api/internal/modules/planning/shared"
	statspkg "welloresto-api/internal/modules/stats"
)

type StatsReader interface {
	GetMerchantTimezone(ctx context.Context, merchantID string) (string, error)
	ListRevenueHTByLocalDay(ctx context.Context, merchantID, tzOffset string, startTimeUTC, endTimeUTC time.Time) ([]statspkg.RevenueHTByLocalDay, error)
}

// SettingsReader gives the performance module read access to the merchant's
// night-shift window and holiday calendar, needed for premium segmentation.
type SettingsReader interface {
	GetOrCreateSettings(ctx context.Context, merchantID string) (*settingspkg.PlanningSettings, error)
	ListPlanningHolidays(ctx context.Context, merchantID string, startDate, endDate time.Time) ([]settingspkg.PlanningHoliday, error)
}

type Repository struct {
	db       *sql.DB
	stats    StatsReader
	settings SettingsReader
}

// plnDayFmt formate une date/timestamp en 'YYYY-MM-DD' selon le dialecte.
func plnDayFmt(expr string) string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "to_char(" + expr + ", 'YYYY-MM-DD')"
	}
	return "DATE_FORMAT(" + expr + ", '%Y-%m-%d')"
}

func NewRepository(db *sql.DB, stats StatsReader, settings SettingsReader) *Repository {
	return &Repository{db: db, stats: stats, settings: settings}
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

// ListPlannedShiftIntervals returns one row per planned shift, ungrouped,
// so premium classification (night/Sunday/holiday) can be computed in Go
// against the merchant's settings — unlike ListPlannedByDayEmployee, no
// per-dialect date-math is needed here, the SQL only fetches raw columns.
func (r *Repository) ListPlannedShiftIntervals(ctx context.Context, merchantID string, fromLocalDay, toLocalDay time.Time) ([]PlannedShiftInterval, error) {
	fromDay := normalizeDateOnlyUTC(fromLocalDay)
	toDay := normalizeDateOnlyUTC(toLocalDay)
	if toDay.Before(fromDay) {
		return []PlannedShiftInterval{}, nil
	}

	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT s.employee_id, s.shift_date, s.start_time, s.end_time, COALESCE(s.break_minutes, 0)
		FROM planning_shifts s
		WHERE s.merchant_id = ?
			AND s.enabled = TRUE
			AND s.status <> 'cancelled'
			AND s.employee_id IS NOT NULL
			AND s.shift_date >= ?
			AND s.shift_date <= ?
		ORDER BY s.employee_id ASC, s.shift_date ASC
	`, merchantID, fromDay.Format("2006-01-02"), toDay.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("list planned shift intervals: %w", err)
	}
	defer rows.Close()

	items := make([]PlannedShiftInterval, 0)
	for rows.Next() {
		var employeeID, startRaw, endRaw string
		var shiftDate models.DateOnly
		var breakMinutes int
		if err := rows.Scan(&employeeID, &shiftDate, &startRaw, &endRaw, &breakMinutes); err != nil {
			return nil, fmt.Errorf("scan planned shift interval: %w", err)
		}
		startAt, endAt, err := buildShiftInterval(shiftDate.Time(), startRaw, endRaw)
		if err != nil {
			return nil, fmt.Errorf("build shift interval: %w", err)
		}
		items = append(items, PlannedShiftInterval{
			EmployeeID:   employeeID,
			StartAt:      startAt,
			EndAt:        endAt,
			BreakMinutes: breakMinutes,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate planned shift intervals: %w", err)
	}

	return items, nil
}

// buildShiftInterval combines a shift's date with its start/end clock
// strings into naive local wall-clock instants, adjusting EndAt +24h for
// overnight shifts (end_time <= start_time) — same rule as
// ListPlannedByDayEmployee's SQL CASE.
func buildShiftInterval(shiftDate time.Time, startRaw, endRaw string) (time.Time, time.Time, error) {
	startAt, err := combineDateAndClock(shiftDate, startRaw)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	endAt, err := combineDateAndClock(shiftDate, endRaw)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if !endAt.After(startAt) {
		endAt = endAt.Add(24 * time.Hour)
	}
	return startAt, endAt, nil
}

func combineDateAndClock(date time.Time, clockRaw string) (time.Time, error) {
	clock, err := sharedpkg.ParsePlanningTime(clockRaw)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(date.Year(), date.Month(), date.Day(), clock.Hour(), clock.Minute(), clock.Second(), 0, time.UTC), nil
}

// ListWorkedEntryIntervals returns one row per closed time entry, ungrouped,
// StartAt/EndAt already converted to the merchant's real IANA location so
// premium classification can compare them against the night window.
func (r *Repository) ListWorkedEntryIntervals(ctx context.Context, merchantID string, fromLocalDay, toLocalDay time.Time) ([]WorkedEntryInterval, error) {
	location, _, startUTC, endUTC, err := r.resolveMerchantRangeBounds(ctx, merchantID, fromLocalDay, toLocalDay)
	if err != nil {
		return nil, err
	}
	if !endUTC.After(startUTC) {
		return []WorkedEntryInterval{}, nil
	}

	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT te.employee_id, te.clock_in_at, te.clock_out_at
		FROM planning_time_entries te
		WHERE te.merchant_id = ?
			AND te.enabled = TRUE
			AND te.clock_out_at IS NOT NULL
			AND te.clock_in_at >= ?
			AND te.clock_in_at < ?
		ORDER BY te.employee_id ASC, te.clock_in_at ASC
	`, merchantID, startUTC, endUTC)
	if err != nil {
		return nil, fmt.Errorf("list worked entry intervals: %w", err)
	}
	defer rows.Close()

	items := make([]WorkedEntryInterval, 0)
	for rows.Next() {
		var employeeID string
		var clockInAt, clockOutAt time.Time
		if err := rows.Scan(&employeeID, &clockInAt, &clockOutAt); err != nil {
			return nil, fmt.Errorf("scan worked entry interval: %w", err)
		}
		items = append(items, WorkedEntryInterval{
			EmployeeID: employeeID,
			StartAt:    clockInAt.In(location),
			EndAt:      clockOutAt.In(location),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate worked entry intervals: %w", err)
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
		SELECT e.id, e.hourly_rate, e.employer_charges_pct, e.sunday_premium, e.night_premium
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
		if err := rows.Scan(&row.EmployeeID, &row.HourlyRateCents, &row.EmployerChargesPct, &row.SundayPremiumEligible, &row.NightPremiumEligible); err != nil {
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
