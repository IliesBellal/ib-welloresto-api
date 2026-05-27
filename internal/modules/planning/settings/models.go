package settings

import "time"

type PlanningSettings struct {
	ID                    string    `json:"id"`
	MerchantID            string    `json:"merchant_id"`
	LaborCountryCode      string    `json:"labor_country_code"`
	MinDailyRestHours     float64   `json:"min_daily_rest_hours"`
	MinBreakMinutes       int       `json:"min_break_minutes"`
	NightShiftStart       string    `json:"night_shift_start"`
	NightShiftEnd         string    `json:"night_shift_end"`
	NightShiftMultiplier  float64   `json:"night_shift_multiplier"`
	HolidayMultiplier     float64   `json:"holiday_multiplier"`
	AllowOverrideWarnings bool      `json:"allow_override_warnings"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type PlanningSettingsUpdateRequest struct {
	LaborCountryCode      *string  `json:"labor_country_code,omitempty"`
	MinDailyRestHours     *float64 `json:"min_daily_rest_hours,omitempty"`
	MinBreakMinutes       *int     `json:"min_break_minutes,omitempty"`
	NightShiftStart       *string  `json:"night_shift_start,omitempty"`
	NightShiftEnd         *string  `json:"night_shift_end,omitempty"`
	NightShiftMultiplier  *float64 `json:"night_shift_multiplier,omitempty"`
	HolidayMultiplier     *float64 `json:"holiday_multiplier,omitempty"`
	AllowOverrideWarnings *bool    `json:"allow_override_warnings,omitempty"`
}

type LaborRule struct {
	CountryCode          string    `json:"country_code"`
	Label                string    `json:"label"`
	MinDailyRestHours    float64   `json:"min_daily_rest_hours"`
	MinBreakMinutes      int       `json:"min_break_minutes"`
	NightShiftStart      string    `json:"night_shift_start"`
	NightShiftEnd        string    `json:"night_shift_end"`
	NightShiftMultiplier float64   `json:"night_shift_multiplier"`
	HolidayMultiplier    float64   `json:"holiday_multiplier"`
	MaxWeeklyHours       float64   `json:"max_weekly_hours"`
	CreatedAt            time.Time `json:"created_at,omitempty"`
	UpdatedAt            time.Time `json:"updated_at,omitempty"`
}
