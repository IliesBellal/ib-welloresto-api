package settings

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/database/dbx"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetOrCreateSettings(ctx context.Context, merchantID string) (*PlanningSettings, error) {
	db := dbx.GetDB(ctx, r.db)

	settings, err := r.GetSettings(ctx, merchantID)
	if err == nil {
		return settings, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	rule, err := r.GetLaborRuleByCountry(ctx, "FR")
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if rule == nil {
		rule = defaultLaborRule("FR")
	}

	now := time.Now().UTC()
	id := helpers.GeneratePrefixedID(helpers.PlanningSettingsIDPrefix)
	_, err = db.ExecContext(ctx, `
		INSERT INTO planning_settings (
			id, merchant_id, labor_country_code, min_daily_rest_hours, min_break_minutes,
			night_shift_start, night_shift_end, night_shift_multiplier, holiday_multiplier,
			allow_override_warnings, attendance_source, shift_swap_approval_mode, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, merchantID, rule.CountryCode, rule.MinDailyRestHours, rule.MinBreakMinutes, rule.NightShiftStart, rule.NightShiftEnd, rule.NightShiftMultiplier, rule.HolidayMultiplier, true, AttendanceSourcePointage, ShiftSwapApprovalModeManagerRequired, now, now)
	if err != nil {
		return nil, err
	}

	return &PlanningSettings{
		ID:                    id,
		MerchantID:            merchantID,
		LaborCountryCode:      rule.CountryCode,
		MinDailyRestHours:     rule.MinDailyRestHours,
		MinBreakMinutes:       rule.MinBreakMinutes,
		NightShiftStart:       rule.NightShiftStart,
		NightShiftEnd:         rule.NightShiftEnd,
		NightShiftMultiplier:  rule.NightShiftMultiplier,
		HolidayMultiplier:     rule.HolidayMultiplier,
		AllowOverrideWarnings: true,
		AttendanceSource:      AttendanceSourcePointage,
		ShiftSwapApprovalMode: ShiftSwapApprovalModeManagerRequired,
		CreatedAt:             now,
		UpdatedAt:             now,
	}, nil
}

func (r *Repository) GetSettings(ctx context.Context, merchantID string) (*PlanningSettings, error) {
	db := dbx.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT id, merchant_id, labor_country_code, min_daily_rest_hours, min_break_minutes,
			night_shift_start, night_shift_end, night_shift_multiplier, holiday_multiplier,
			allow_override_warnings, attendance_source, shift_swap_approval_mode, created_at, updated_at
		FROM planning_settings
		WHERE merchant_id = ? AND enabled = TRUE
		LIMIT 1
	`, merchantID)

	var item PlanningSettings
	if err := row.Scan(
		&item.ID,
		&item.MerchantID,
		&item.LaborCountryCode,
		&item.MinDailyRestHours,
		&item.MinBreakMinutes,
		&item.NightShiftStart,
		&item.NightShiftEnd,
		&item.NightShiftMultiplier,
		&item.HolidayMultiplier,
		&item.AllowOverrideWarnings,
		&item.AttendanceSource,
		&item.ShiftSwapApprovalMode,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, err
	}

	return &item, nil
}

func (r *Repository) UpdateSettings(ctx context.Context, merchantID string, req PlanningSettingsUpdateRequest) (*PlanningSettings, error) {
	db := dbx.GetDB(ctx, r.db)
	current, err := r.GetOrCreateSettings(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	if req.LaborCountryCode != nil {
		current.LaborCountryCode = strings.ToUpper(strings.TrimSpace(*req.LaborCountryCode))
	}
	if req.MinDailyRestHours != nil {
		current.MinDailyRestHours = *req.MinDailyRestHours
	}
	if req.MinBreakMinutes != nil {
		current.MinBreakMinutes = *req.MinBreakMinutes
	}
	if req.NightShiftStart != nil {
		current.NightShiftStart = strings.TrimSpace(*req.NightShiftStart)
	}
	if req.NightShiftEnd != nil {
		current.NightShiftEnd = strings.TrimSpace(*req.NightShiftEnd)
	}
	if req.NightShiftMultiplier != nil {
		current.NightShiftMultiplier = *req.NightShiftMultiplier
	}
	if req.HolidayMultiplier != nil {
		current.HolidayMultiplier = *req.HolidayMultiplier
	}
	if req.AllowOverrideWarnings != nil {
		current.AllowOverrideWarnings = *req.AllowOverrideWarnings
	}
	if req.AttendanceSource != nil {
		current.AttendanceSource = NormalizeAttendanceSource(*req.AttendanceSource)
	}
	if req.ShiftSwapApprovalMode != nil {
		current.ShiftSwapApprovalMode = NormalizeShiftSwapApprovalMode(*req.ShiftSwapApprovalMode)
	}

	current.UpdatedAt = time.Now().UTC()
	_, err = db.ExecContext(ctx, `
		UPDATE planning_settings
		SET labor_country_code = ?, min_daily_rest_hours = ?, min_break_minutes = ?,
			night_shift_start = ?, night_shift_end = ?, night_shift_multiplier = ?,
			holiday_multiplier = ?, allow_override_warnings = ?, attendance_source = ?, shift_swap_approval_mode = ?, updated_at = ?
		WHERE merchant_id = ? AND enabled = TRUE
	`, current.LaborCountryCode, current.MinDailyRestHours, current.MinBreakMinutes, current.NightShiftStart, current.NightShiftEnd, current.NightShiftMultiplier, current.HolidayMultiplier, current.AllowOverrideWarnings, current.AttendanceSource, current.ShiftSwapApprovalMode, current.UpdatedAt, merchantID)
	if err != nil {
		return nil, err
	}

	return current, nil
}

func (r *Repository) GetLaborRuleByCountry(ctx context.Context, countryCode string) (*LaborRule, error) {
	db := dbx.GetDB(ctx, r.db)
	row := db.QueryRowContext(ctx, `
		SELECT country_code, label, min_daily_rest_hours, min_break_minutes, night_shift_start, night_shift_end,
			night_shift_multiplier, holiday_multiplier, max_weekly_hours, created_at, updated_at
		FROM labor_rules
		WHERE country_code = ? AND enabled = TRUE
		LIMIT 1
	`, strings.ToUpper(strings.TrimSpace(countryCode)))
	var item LaborRule
	if err := row.Scan(&item.CountryCode, &item.Label, &item.MinDailyRestHours, &item.MinBreakMinutes, &item.NightShiftStart, &item.NightShiftEnd, &item.NightShiftMultiplier, &item.HolidayMultiplier, &item.MaxWeeklyHours, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func defaultLaborRule(countryCode string) *LaborRule {
	return &LaborRule{
		CountryCode:          strings.ToUpper(strings.TrimSpace(countryCode)),
		Label:                "France",
		MinDailyRestHours:    11,
		MinBreakMinutes:      45,
		NightShiftStart:      "22:00:00",
		NightShiftEnd:        "06:00:00",
		NightShiftMultiplier: 1.25,
		HolidayMultiplier:    2,
		MaxWeeklyHours:       48,
	}
}
