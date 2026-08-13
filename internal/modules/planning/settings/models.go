package settings

import (
	"strings"
	"time"
)

const (
	AttendanceSourcePointage                    = "pointage"
	AttendanceSourcePlanning                    = "planning"
	ShiftSwapApprovalModeManagerRequired        = "manager_required"
	ShiftSwapApprovalModeTargetEmployeeRequired = "target_employee_required"
	PremiumCumulationModeAdditive               = "additive"
	PremiumCumulationModeHighest                = "highest"
	PremiumCumulationModeFixed                  = "fixed"
)

// DefaultSundayMultiplier is used when a merchant's settings row is first created.
// Unlike NightShiftMultiplier/HolidayMultiplier, it has no country labor-rule default:
// French law does not mandate a standard Sunday pay premium, so it starts neutral.
const DefaultSundayMultiplier = 1.0

// DefaultPremiumCumulationMode mirrors the legal default absent an explicit
// collective-agreement clause: the single highest applicable rate wins,
// rather than stacking night + Sunday additively.
const DefaultPremiumCumulationMode = PremiumCumulationModeHighest

func NormalizeAttendanceSource(source string) string {
	return strings.ToLower(strings.TrimSpace(source))
}

func IsValidAttendanceSource(source string) bool {
	switch NormalizeAttendanceSource(source) {
	case AttendanceSourcePointage, AttendanceSourcePlanning:
		return true
	default:
		return false
	}
}

func NormalizeShiftSwapApprovalMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}

func IsValidShiftSwapApprovalMode(mode string) bool {
	switch NormalizeShiftSwapApprovalMode(mode) {
	case ShiftSwapApprovalModeManagerRequired, ShiftSwapApprovalModeTargetEmployeeRequired:
		return true
	default:
		return false
	}
}

func NormalizePremiumCumulationMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}

func IsValidPremiumCumulationMode(mode string) bool {
	switch NormalizePremiumCumulationMode(mode) {
	case PremiumCumulationModeAdditive, PremiumCumulationModeHighest, PremiumCumulationModeFixed:
		return true
	default:
		return false
	}
}

type PlanningSettings struct {
	ID                   string  `json:"id"`
	MerchantID           string  `json:"merchant_id"`
	LaborCountryCode     string  `json:"labor_country_code"`
	MinDailyRestHours    float64 `json:"min_daily_rest_hours"`
	MinBreakMinutes      int     `json:"min_break_minutes"`
	NightShiftStart      string  `json:"night_shift_start"`
	NightShiftEnd        string  `json:"night_shift_end"`
	NightShiftMultiplier float64 `json:"night_shift_multiplier"`
	HolidayMultiplier    float64 `json:"holiday_multiplier"`
	SundayMultiplier     float64 `json:"sunday_multiplier"`
	// PremiumCumulationMode governs how night/Sunday/holiday premiums combine
	// when they overlap on the same worked hour: additive (rates stack),
	// highest (single max rate wins, the legal default absent a convention
	// clause), or fixed (NightSundayCombinedMultiplier applies instead of
	// either individual rate).
	PremiumCumulationMode         string   `json:"premium_cumulation_mode"`
	NightSundayCombinedMultiplier *float64 `json:"night_sunday_combined_multiplier,omitempty"`
	AllowOverrideWarnings         bool     `json:"allow_override_warnings"`
	AttendanceSource              string   `json:"attendance_source"`
	ShiftSwapApprovalMode         string   `json:"shift_swap_approval_mode"`
	// PlanningSMSNotificationsEnabled gates every planning SMS path.
	// When false, publication notifications remain email-only and no fallback
	// inline SMS is sent to inactive employees.
	PlanningSMSNotificationsEnabled     bool      `json:"planning_sms_notifications_enabled"`
	PlanningSMSNotificationsDescription string    `json:"planning_sms_notifications_description"`
	CreatedAt                           time.Time `json:"created_at"`
	UpdatedAt                           time.Time `json:"updated_at"`
}

type PlanningSettingsUpdateRequest struct {
	LaborCountryCode                *string  `json:"labor_country_code,omitempty"`
	MinDailyRestHours               *float64 `json:"min_daily_rest_hours,omitempty"`
	MinBreakMinutes                 *int     `json:"min_break_minutes,omitempty"`
	NightShiftStart                 *string  `json:"night_shift_start,omitempty"`
	NightShiftEnd                   *string  `json:"night_shift_end,omitempty"`
	NightShiftMultiplier            *float64 `json:"night_shift_multiplier,omitempty"`
	HolidayMultiplier               *float64 `json:"holiday_multiplier,omitempty"`
	SundayMultiplier                *float64 `json:"sunday_multiplier,omitempty"`
	PremiumCumulationMode           *string  `json:"premium_cumulation_mode,omitempty"`
	NightSundayCombinedMultiplier   *float64 `json:"night_sunday_combined_multiplier,omitempty"`
	AllowOverrideWarnings           *bool    `json:"allow_override_warnings,omitempty"`
	AttendanceSource                *string  `json:"attendance_source,omitempty"`
	ShiftSwapApprovalMode           *string  `json:"shift_swap_approval_mode,omitempty"`
	PlanningSMSNotificationsEnabled *bool    `json:"planning_sms_notifications_enabled,omitempty"`
}

const PlanningSMSNotificationsDescription = "If disabled, planning publication notifications stay email-only and no SMS fallback is sent to inactive employees."

type PlanningHoliday struct {
	OverrideID     *string   `json:"override_id,omitempty"`
	Date           time.Time `json:"date"`
	Label          *string   `json:"label,omitempty"`
	IsLegalHoliday bool      `json:"is_legal_holiday"`
	CountAsHoliday bool      `json:"count_as_holiday"`
	IsOpen         *bool     `json:"is_open,omitempty"`
}

type PlanningHolidayListFilters struct {
	StartDate string
	EndDate   string
}

type PlanningHolidayOverridePatchRequest struct {
	Label               *string `json:"label,omitempty"`
	IsOpen              *bool   `json:"is_open,omitempty"`
	CountAsHoliday      *bool   `json:"count_as_holiday,omitempty"`
	ClearLabel          *bool   `json:"clear_label,omitempty"`
	ClearIsOpen         *bool   `json:"clear_is_open,omitempty"`
	ClearCountAsHoliday *bool   `json:"clear_count_as_holiday,omitempty"`
}

// PlanningVacationPeriod force la fermeture de l'etablissement (statut POS,
// cf. pos.GetPOSStatus) sur toute la plage [StartAt, EndAt], en plus des
// horaires d'ouverture habituels — meme mecanisme que PlanningHoliday mais
// sur une periode plutot qu'une seule date.
//
// StartAt/EndAt sont des chaines "YYYY-MM-DD HH:MM:SS" en heure locale du
// marchand, pas des time.Time : meme convention que
// models.POSHoursOfOperation.ValidFrom/ValidTo, qui evite toute ambiguite de
// fuseau (aucune conversion Go, la valeur saisie est stockee et comparee telle
// quelle).
type PlanningVacationPeriod struct {
	ID        string    `json:"id"`
	Label     *string   `json:"label,omitempty"`
	StartAt   string    `json:"start_at"`
	EndAt     string    `json:"end_at"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PlanningVacationPeriodCreateRequest struct {
	Label   *string `json:"label,omitempty"`
	StartAt string  `json:"start_at"`
	EndAt   string  `json:"end_at"`
}

type PlanningVacationPeriodUpdateRequest struct {
	Label   *string `json:"label,omitempty"`
	StartAt *string `json:"start_at,omitempty"`
	EndAt   *string `json:"end_at,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
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
