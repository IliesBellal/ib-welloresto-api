package performance

import (
	"math"
	"testing"

	settingspkg "welloresto-api/internal/modules/planning/settings"
)

func TestComputeDayMetrics_NoPremiumEquivalence(t *testing.T) {
	// Non-regression: when everything is classified Normal, the new
	// weighted-hours formula must reproduce the old
	// workedDisplayHours*rate*(1+charges) result exactly.
	day := RawDayMetrics{
		LocalDay: "2026-07-27",
		Employees: []RawDayEmployeeMetrics{
			{
				EmployeeID:         "emp-1",
				PlannedMinutes:     480,
				WorkedSeconds:      8 * 3600,
				HourlyRateCents:    1500,
				EmployerChargesPct: 40,
				WorkedPremium:      PremiumSegments{NormalSeconds: 8 * 3600},
			},
		},
	}
	config := PremiumConfig{NightShiftMultiplier: 1.25, SundayMultiplier: 1.20, CumulationMode: settingspkg.PremiumCumulationModeHighest}

	got := computeDayMetrics(day, config, map[string]struct{}{})

	wantCents := int64(math.Round(8.0 * 1500 * 1.40))
	if got.Period.PayrollCostLoadedCents != wantCents {
		t.Fatalf("PayrollCostLoadedCents = %d, want %d", got.Period.PayrollCostLoadedCents, wantCents)
	}
	if got.Period.WorkedHours != 8.0 {
		t.Fatalf("WorkedHours = %v, want 8.0", got.Period.WorkedHours)
	}
}

func TestComputeDayMetrics_FallsBackToPlannedPremiumWhenNoWorkedSeconds(t *testing.T) {
	// WorkedSeconds<=0 must switch to PlannedPremium, mirroring the existing
	// workedDisplayHours fallback exactly (same condition).
	day := RawDayMetrics{
		LocalDay: "2026-07-27",
		Employees: []RawDayEmployeeMetrics{
			{
				EmployeeID:           "emp-1",
				PlannedMinutes:       480,
				WorkedSeconds:        0,
				HourlyRateCents:      1000,
				NightPremiumEligible: true,
				PlannedPremium:       PremiumSegments{NightSeconds: 8 * 3600},
				WorkedPremium:        PremiumSegments{NormalSeconds: 999 * 3600}, // must be ignored
			},
		},
	}
	config := PremiumConfig{NightShiftMultiplier: 1.25, SundayMultiplier: 1.0, CumulationMode: settingspkg.PremiumCumulationModeHighest}

	got := computeDayMetrics(day, config, map[string]struct{}{})

	wantCents := int64(math.Round(8.0 * 1.25 * 1000))
	if got.Period.PayrollCostLoadedCents != wantCents {
		t.Fatalf("PayrollCostLoadedCents = %d, want %d (should use PlannedPremium, not WorkedPremium)", got.Period.PayrollCostLoadedCents, wantCents)
	}
}

func TestComputeDayMetrics_NoRateStillGatesRegardlessOfPremium(t *testing.T) {
	membersWithoutRate := map[string]struct{}{}
	day := RawDayMetrics{
		LocalDay: "2026-07-27",
		Employees: []RawDayEmployeeMetrics{
			{
				EmployeeID:           "emp-1",
				WorkedSeconds:        3600,
				HourlyRateCents:      0,
				NightPremiumEligible: true,
				WorkedPremium:        PremiumSegments{NightSeconds: 3600},
			},
		},
	}
	config := PremiumConfig{NightShiftMultiplier: 2.0}

	got := computeDayMetrics(day, config, membersWithoutRate)

	if got.Period.PayrollCostLoadedCents != 0 {
		t.Fatalf("PayrollCostLoadedCents = %d, want 0 (no hourly rate)", got.Period.PayrollCostLoadedCents)
	}
	if _, ok := membersWithoutRate["emp-1"]; !ok {
		t.Fatal("expected emp-1 to be flagged in membersWithoutRate")
	}
	if !got.MSIncomplete {
		t.Fatal("expected MSIncomplete = true")
	}
}

func TestComputeDayMetrics_MultipleEmployeesAggregate(t *testing.T) {
	day := RawDayMetrics{
		LocalDay: "2026-07-27",
		Employees: []RawDayEmployeeMetrics{
			{
				EmployeeID:      "emp-1",
				WorkedSeconds:   4 * 3600,
				HourlyRateCents: 1000,
				WorkedPremium:   PremiumSegments{NormalSeconds: 4 * 3600},
			},
			{
				EmployeeID:           "emp-2",
				WorkedSeconds:        4 * 3600,
				HourlyRateCents:      1000,
				NightPremiumEligible: true,
				WorkedPremium:        PremiumSegments{NightSeconds: 4 * 3600},
			},
		},
	}
	config := PremiumConfig{NightShiftMultiplier: 1.5, SundayMultiplier: 1.0}

	got := computeDayMetrics(day, config, map[string]struct{}{})

	want := int64(math.Round(4*1000 + 4*1.5*1000))
	if got.Period.PayrollCostLoadedCents != want {
		t.Fatalf("PayrollCostLoadedCents = %d, want %d", got.Period.PayrollCostLoadedCents, want)
	}
}

func TestComputeDayMetrics_PremiumDoesNotLeakIntoDisplayedHours(t *testing.T) {
	// PayrollCostLoadedCents is weighted, but WorkedHours/PlannedHours
	// (used for hours_delta / revenue_per_hour_cents) must stay raw.
	day := RawDayMetrics{
		LocalDay: "2026-07-27",
		Employees: []RawDayEmployeeMetrics{
			{
				EmployeeID:           "emp-1",
				WorkedSeconds:        4 * 3600,
				HourlyRateCents:      1000,
				NightPremiumEligible: true,
				WorkedPremium:        PremiumSegments{NightSeconds: 4 * 3600},
			},
		},
	}
	config := PremiumConfig{NightShiftMultiplier: 1.25}

	got := computeDayMetrics(day, config, map[string]struct{}{})

	if got.Period.WorkedHours != 4.0 {
		t.Fatalf("WorkedHours = %v, want 4.0 (raw, not weighted)", got.Period.WorkedHours)
	}
	wantPayroll := int64(math.Round(4 * 1.25 * 1000))
	if got.Period.PayrollCostLoadedCents != wantPayroll {
		t.Fatalf("PayrollCostLoadedCents = %d, want %d (weighted)", got.Period.PayrollCostLoadedCents, wantPayroll)
	}
}
