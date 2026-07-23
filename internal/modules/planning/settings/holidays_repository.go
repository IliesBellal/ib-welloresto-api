package settings

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/database/dbx"
)

type planningHolidayOverrideRecord struct {
	ID             string
	MerchantID     string
	HolidayDate    time.Time
	Label          *string
	IsOpen         *bool
	CountAsHoliday *bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

func (r *Repository) ListPlanningHolidays(ctx context.Context, merchantID string, startDate, endDate time.Time) ([]PlanningHoliday, error) {
	countryCode, err := r.getEffectiveLaborCountryCode(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `
		SELECT o.id, d.holiday_date, COALESCE(o.label, hc.label),
			CASE WHEN hc.holiday_date IS NOT NULL THEN TRUE ELSE FALSE END AS is_legal_holiday,
			CASE
				WHEN o.count_as_holiday IS NOT NULL THEN o.count_as_holiday
				WHEN hc.holiday_date IS NOT NULL THEN TRUE
				ELSE FALSE
			END AS count_as_holiday,
			o.is_open
		FROM (
			SELECT holiday_date
			FROM holiday_calendar
			WHERE country_code = ? AND enabled = TRUE AND holiday_date >= ? AND holiday_date <= ?
			UNION
			SELECT holiday_date
			FROM planning_holiday_overrides
			WHERE merchant_id = ? AND enabled = TRUE AND holiday_date >= ? AND holiday_date <= ?
		) d
		LEFT JOIN holiday_calendar hc ON hc.country_code = ? AND hc.holiday_date = d.holiday_date AND hc.enabled = TRUE
		LEFT JOIN planning_holiday_overrides o ON o.merchant_id = ? AND o.holiday_date = d.holiday_date AND o.enabled = TRUE
		ORDER BY d.holiday_date ASC
	`, countryCode, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"), merchantID, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"), countryCode, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PlanningHoliday, 0)
	for rows.Next() {
		item, scanErr := scanPlanningHoliday(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ResolvePlanningHoliday(ctx context.Context, merchantID string, holidayDate time.Time) (*PlanningHoliday, error) {
	countryCode, err := r.getEffectiveLaborCountryCode(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	db := dbx.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT o.id, d.holiday_date, COALESCE(o.label, hc.label),
			CASE WHEN hc.holiday_date IS NOT NULL THEN TRUE ELSE FALSE END AS is_legal_holiday,
			CASE
				WHEN o.count_as_holiday IS NOT NULL THEN o.count_as_holiday
				WHEN hc.holiday_date IS NOT NULL THEN TRUE
				ELSE FALSE
			END AS count_as_holiday,
			o.is_open
		FROM (SELECT CAST(? AS DATE) AS holiday_date) d
		LEFT JOIN holiday_calendar hc ON hc.country_code = ? AND hc.holiday_date = d.holiday_date AND hc.enabled = TRUE
		LEFT JOIN planning_holiday_overrides o ON o.merchant_id = ? AND o.holiday_date = d.holiday_date AND o.enabled = TRUE
		LIMIT 1
	`, holidayDate.Format("2006-01-02"), countryCode, merchantID)
	return scanPlanningHolidayRow(row)
}

func (r *Repository) GetPlanningHolidayOverrideByDate(ctx context.Context, merchantID string, holidayDate time.Time) (*planningHolidayOverrideRecord, error) {
	db := dbx.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, holiday_date, label, is_open, count_as_holiday, created_at, updated_at, deleted_at
		FROM planning_holiday_overrides
		WHERE merchant_id = ? AND holiday_date = ? AND enabled = TRUE
		LIMIT 1
	`, merchantID, holidayDate.Format("2006-01-02"))
	return scanPlanningHolidayOverrideRow(row)
}

func (r *Repository) CreatePlanningHolidayOverride(ctx context.Context, merchantID string, override planningHolidayOverrideRecord) (*planningHolidayOverrideRecord, error) {
	db := dbx.GetDB(ctx, r.db)
	now := time.Now().UTC()
	override.ID = helpers.GeneratePrefixedID(helpers.PlanningHolidayIDPrefix)
	override.MerchantID = merchantID
	override.CreatedAt = now
	override.UpdatedAt = now
	_, err := db.ExecContext(ctx, `
		INSERT INTO planning_holiday_overrides (
			id, merchant_id, holiday_date, label, is_open, count_as_holiday, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, TRUE, ?, ?)
	`, override.ID, override.MerchantID, override.HolidayDate.Format("2006-01-02"), override.Label, override.IsOpen, override.CountAsHoliday, override.CreatedAt, override.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &override, nil
}

func (r *Repository) UpdatePlanningHolidayOverride(ctx context.Context, merchantID string, override planningHolidayOverrideRecord) (*planningHolidayOverrideRecord, error) {
	db := dbx.GetDB(ctx, r.db)
	override.UpdatedAt = time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_holiday_overrides
		SET label = ?, is_open = ?, count_as_holiday = ?, updated_at = ?
		WHERE merchant_id = ? AND holiday_date = ? AND enabled = TRUE
	`, override.Label, override.IsOpen, override.CountAsHoliday, override.UpdatedAt, merchantID, override.HolidayDate.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, sql.ErrNoRows
	}
	return &override, nil
}

func (r *Repository) SoftDeletePlanningHolidayOverride(ctx context.Context, merchantID string, holidayDate time.Time) error {
	db := dbx.GetDB(ctx, r.db)
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx, `
		UPDATE planning_holiday_overrides
		SET enabled = FALSE, deleted_at = ?, updated_at = ?
		WHERE merchant_id = ? AND holiday_date = ? AND enabled = TRUE
	`, now, now, merchantID, holidayDate.Format("2006-01-02"))
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) getEffectiveLaborCountryCode(ctx context.Context, merchantID string) (string, error) {
	settings, err := r.GetSettings(ctx, merchantID)
	if err == sql.ErrNoRows {
		return "FR", nil
	}
	if err != nil {
		return "", err
	}
	code := strings.ToUpper(strings.TrimSpace(settings.LaborCountryCode))
	if code == "" {
		return "FR", nil
	}
	return code, nil
}

type planningHolidayScannable interface {
	Scan(dest ...any) error
}

func scanPlanningHolidayRow(row planningHolidayScannable) (*PlanningHoliday, error) {
	item := &PlanningHoliday{}
	var overrideID, label sql.NullString
	var holidayDateRaw any
	var isOpen sql.NullBool
	if err := row.Scan(&overrideID, &holidayDateRaw, &label, &item.IsLegalHoliday, &item.CountAsHoliday, &isOpen); err != nil {
		return nil, err
	}
	holidayDate, err := parsePlanningHolidayDate(holidayDateRaw)
	if err != nil {
		return nil, err
	}
	item.Date = holidayDate
	if overrideID.Valid {
		item.OverrideID = &overrideID.String
	}
	if label.Valid {
		item.Label = &label.String
	}
	if isOpen.Valid {
		item.IsOpen = &isOpen.Bool
	}
	return item, nil
}

func scanPlanningHoliday(rows planningHolidayScannable) (*PlanningHoliday, error) {
	return scanPlanningHolidayRow(rows)
}

func scanPlanningHolidayOverrideRow(row planningHolidayScannable) (*planningHolidayOverrideRecord, error) {
	item := &planningHolidayOverrideRecord{}
	var label sql.NullString
	var holidayDateRaw any
	var isOpen, countAsHoliday sql.NullBool
	var deletedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.MerchantID, &holidayDateRaw, &label, &isOpen, &countAsHoliday, &item.CreatedAt, &item.UpdatedAt, &deletedAt); err != nil {
		return nil, err
	}
	holidayDate, err := parsePlanningHolidayDate(holidayDateRaw)
	if err != nil {
		return nil, err
	}
	item.HolidayDate = holidayDate
	if label.Valid {
		item.Label = &label.String
	}
	if isOpen.Valid {
		item.IsOpen = &isOpen.Bool
	}
	if countAsHoliday.Valid {
		item.CountAsHoliday = &countAsHoliday.Bool
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		item.DeletedAt = &t
	}
	return item, nil
}

func parsePlanningHolidayDate(raw any) (time.Time, error) {
	switch value := raw.(type) {
	case time.Time:
		return value.UTC(), nil
	case []byte:
		return parsePlanningHolidayDateString(string(value))
	case string:
		return parsePlanningHolidayDateString(value)
	case nil:
		return time.Time{}, fmt.Errorf("scan planning holiday date: unexpected NULL value")
	default:
		return time.Time{}, fmt.Errorf("scan planning holiday date: unsupported type %T", raw)
	}
}

func parsePlanningHolidayDateString(raw string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, fmt.Errorf("scan planning holiday date: %w", err)
	}
	return parsed.UTC(), nil
}
