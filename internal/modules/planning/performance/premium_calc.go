package performance

import (
	"math"

	settingspkg "welloresto-api/internal/modules/planning/settings"
)

// PremiumConfig captures the merchant-level rates needed to weight worked/
// planned hours by premium classification. Sourced from planning_settings.
type PremiumConfig struct {
	NightShiftMultiplier          float64
	SundayMultiplier              float64
	CumulationMode                string
	NightSundayCombinedMultiplier *float64
}

// weightedPremiumHours converts a PremiumSegments bucket set into a single
// hours figure, weighting the Night/Sunday/NightSunday buckets by the
// applicable multiplier — gated by the employee's own eligibility
// (sunday_premium/night_premium). Holiday is deliberately not applied yet:
// there is no employee-level eligibility flag for it, and the mechanism
// itself is contested for some conventions (see "Reporté — Jours fériés
// HCR" in docs/PLANNING_DECISIONS.md).
func weightedPremiumHours(segments PremiumSegments, sundayEligible, nightEligible bool, config PremiumConfig) float64 {
	hours := func(seconds int64) float64 { return float64(seconds) / 3600.0 }

	total := hours(segments.NormalSeconds)

	nightMultiplier := 1.0
	if nightEligible {
		nightMultiplier = config.NightShiftMultiplier
	}
	total += hours(segments.NightSeconds) * nightMultiplier

	sundayMultiplier := 1.0
	if sundayEligible {
		sundayMultiplier = config.SundayMultiplier
	}
	total += hours(segments.SundaySeconds) * sundayMultiplier

	total += hours(segments.NightSundaySeconds) * combinedMultiplier(sundayEligible, nightEligible, config)

	return total
}

// combinedMultiplier resolves the rate applied to hours that are
// simultaneously night AND Sunday, per the merchant's premium_cumulation_mode.
// If only one of the two premiums is eligible for this employee, the other
// rate is irrelevant — this degrades to that single rate rather than
// invoking cumulation at all (there is nothing to cumulate).
func combinedMultiplier(sundayEligible, nightEligible bool, config PremiumConfig) float64 {
	switch {
	case nightEligible && sundayEligible:
		switch config.CumulationMode {
		case settingspkg.PremiumCumulationModeAdditive:
			return config.NightShiftMultiplier + config.SundayMultiplier - 1.0
		case settingspkg.PremiumCumulationModeFixed:
			if config.NightSundayCombinedMultiplier != nil {
				return *config.NightSundayCombinedMultiplier
			}
			// "fixed" chosen without a combined rate configured — fall
			// back to "highest" rather than silently using 1.0 or
			// erroring out the whole performance dashboard.
			return math.Max(config.NightShiftMultiplier, config.SundayMultiplier)
		default: // settingspkg.PremiumCumulationModeHighest, or unrecognized
			return math.Max(config.NightShiftMultiplier, config.SundayMultiplier)
		}
	case nightEligible:
		return config.NightShiftMultiplier
	case sundayEligible:
		return config.SundayMultiplier
	default:
		return 1.0
	}
}
