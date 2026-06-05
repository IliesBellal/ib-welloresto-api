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

type RevenueByDayRow struct {
	LocalDay       string
	RevenueHTCents int64
}

type EmployeeRateRow struct {
	EmployeeID         string
	HourlyRateCents    int64
	EmployerChargesPct float64
}

type RawDayEmployeeMetrics struct {
	EmployeeID         string  `json:"employee_id"`
	PlannedMinutes     int64   `json:"planned_minutes"`
	PlannedHours       float64 `json:"planned_hours"`
	WorkedSeconds      int64   `json:"worked_seconds"`
	WorkedHours        float64 `json:"worked_hours"`
	HourlyRateCents    int64   `json:"hourly_rate_cents"`
	EmployerChargesPct float64 `json:"employer_charges_pct"`
}

type RawDayMetrics struct {
	LocalDay       string                  `json:"local_day"`
	RevenueHTCents int64                   `json:"revenue_ht_cents"`
	Headcount      int                     `json:"headcount"`
	Employees      []RawDayEmployeeMetrics `json:"employees"`
}

type RawPerformanceResponse struct {
	FromLocalDay string          `json:"from"`
	ToLocalDay   string          `json:"to"`
	GeneratedAt  time.Time       `json:"generated_at"`
	Days         []RawDayMetrics `json:"days"`
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

type PerformanceResponse struct {
	From           string              `json:"from"`
	To             string              `json:"to"`
	Granularity    string              `json:"granularity"`
	Periods        []PerformancePeriod `json:"periods"`
	Totals         PerformancePeriod   `json:"totals"`
	PreviousPeriod *PerformancePeriod  `json:"previous_period"`
	Warnings       PerformanceWarnings `json:"warnings"`
}
