package performance

import "time"

type PlannedByDayEmployeeRow struct {
	LocalDay       string
	EmployeeID     string
	PlannedMinutes int64
}

type WorkedRawByDayEmployeeRow struct {
	LocalDay      string
	EmployeeID    string
	WorkedSeconds int64
}

// PlannedShiftInterval is one raw planned shift, kept ungrouped so its
// premium classification (night/Sunday/holiday) can be computed in Go.
// StartAt/EndAt are naive local wall-clock instants (shift_date combined
// with start_time/end_time, Location=UTC used only as a neutral label —
// see segmentInterval). EndAt is already adjusted +24h for overnight shifts.
type PlannedShiftInterval struct {
	EmployeeID   string
	StartAt      time.Time
	EndAt        time.Time
	BreakMinutes int
}

// WorkedEntryInterval is one raw clocked time entry. StartAt/EndAt are
// already converted to the merchant's real IANA location.
type WorkedEntryInterval struct {
	EmployeeID string
	StartAt    time.Time
	EndAt      time.Time
}

type RevenueByDayRow struct {
	LocalDay       string
	RevenueHTCents int64
}

type EmployeeRateRow struct {
	EmployeeID            string
	HourlyRateCents       int64
	EmployerChargesPct    float64
	SundayPremiumEligible bool
	NightPremiumEligible  bool
}

type RawDayEmployeeMetrics struct {
	EmployeeID         string  `json:"employee_id"`
	PlannedMinutes     int64   `json:"planned_minutes"`
	PlannedHours       float64 `json:"planned_hours"`
	WorkedSeconds      int64   `json:"worked_seconds"`
	WorkedHours        float64 `json:"worked_hours"`
	HourlyRateCents    int64   `json:"hourly_rate_cents"`
	EmployerChargesPct float64 `json:"employer_charges_pct"`

	// Eligibility gates (employees.sunday_premium/night_premium) — whether
	// this employee's Night/Sunday buckets get a premium rate at all.
	SundayPremiumEligible bool `json:"sunday_premium_eligible"`
	NightPremiumEligible  bool `json:"night_premium_eligible"`

	// Premium segmentation (nuit/dimanche/férié), consumed by
	// computeDayMetrics/weightedPremiumHours to weight PayrollCostLoadedCents.
	PlannedPremium PremiumSegments `json:"planned_premium"`
	WorkedPremium  PremiumSegments `json:"worked_premium"`
}

type RawDayMetrics struct {
	LocalDay             string                  `json:"local_day"`
	RevenueHTCents       int64                   `json:"revenue_ht_cents"`
	RevenueForecastCents *int64                  `json:"revenue_forecast_cents"`
	Headcount            int                     `json:"headcount"`
	Employees            []RawDayEmployeeMetrics `json:"employees"`
}

type RawPerformanceResponse struct {
	FromLocalDay string          `json:"from"`
	ToLocalDay   string          `json:"to"`
	GeneratedAt  time.Time       `json:"generated_at"`
	Days         []RawDayMetrics `json:"days"`
	Premium      PremiumConfig   `json:"premium_config"`
}

type PerformancePeriod struct {
	PeriodStart            string   `json:"period_start"`
	PeriodEnd              string   `json:"period_end"`
	Label                  string   `json:"label"`
	RevenueActualCents     int64    `json:"revenue_actual_cents"`
	RevenueForecastCents   *int64   `json:"revenue_forecast_cents"`
	PlannedHours           float64  `json:"planned_hours"`
	WorkedHours            float64  `json:"worked_hours"`
	Headcount              int      `json:"headcount"`
	PayrollCostLoadedCents int64    `json:"payroll_cost_loaded_cents"`
	PayrollRatio           *float64 `json:"payroll_ratio"`
	RevenuePerHourCents    *float64 `json:"revenue_per_hour_cents"`
	HoursDelta             float64  `json:"hours_delta"`
}

type PerformanceWarnings struct {
	MembersWithoutRate int `json:"members_without_rate"`
}

type PerformancePreviousPeriod struct {
	From    string              `json:"from"`
	To      string              `json:"to"`
	Periods []PerformancePeriod `json:"periods"`
	Totals  PerformancePeriod   `json:"totals"`
}

type PerformanceResponse struct {
	From           string                     `json:"from"`
	To             string                     `json:"to"`
	Granularity    string                     `json:"granularity"`
	Periods        []PerformancePeriod        `json:"periods"`
	Totals         PerformancePeriod          `json:"totals"`
	PreviousPeriod *PerformancePreviousPeriod `json:"previous_period"`
	Warnings       PerformanceWarnings        `json:"warnings"`
}
