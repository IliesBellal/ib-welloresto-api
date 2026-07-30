package performance

import (
	"testing"

	settingspkg "welloresto-api/internal/modules/planning/settings"
)

const floatEpsilon = 1e-9

func almostEqual(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < floatEpsilon
}

func TestWeightedPremiumHours(t *testing.T) {
	baseConfig := PremiumConfig{
		NightShiftMultiplier: 1.25,
		SundayMultiplier:     1.20,
		CumulationMode:       settingspkg.PremiumCumulationModeHighest,
	}
	fixedRate := 1.5

	tests := []struct {
		name           string
		segments       PremiumSegments
		sundayEligible bool
		nightEligible  bool
		config         PremiumConfig
		want           float64
	}{
		{
			name:     "all normal, no eligibility",
			segments: PremiumSegments{NormalSeconds: 8 * 3600},
			config:   baseConfig,
			want:     8.0,
		},
		{
			name:          "pure night, eligible",
			segments:      PremiumSegments{NightSeconds: 5 * 3600},
			nightEligible: true,
			config:        baseConfig,
			want:          6.25,
		},
		{
			name:          "pure night, NOT eligible -> no premium despite classification",
			segments:      PremiumSegments{NightSeconds: 5 * 3600},
			nightEligible: false,
			config:        baseConfig,
			want:          5.0,
		},
		{
			name:           "pure sunday, eligible",
			segments:       PremiumSegments{SundaySeconds: 6 * 3600},
			sundayEligible: true,
			config:         baseConfig,
			want:           7.2,
		},
		{
			name:           "pure sunday, NOT eligible",
			segments:       PremiumSegments{SundaySeconds: 6 * 3600},
			sundayEligible: false,
			config:         baseConfig,
			want:           6.0,
		},
		{
			name:           "night+sunday overlap, both eligible, highest",
			segments:       PremiumSegments{NightSundaySeconds: 4 * 3600},
			sundayEligible: true,
			nightEligible:  true,
			config:         baseConfig,
			want:           5.0, // max(1.25,1.20)=1.25 * 4h
		},
		{
			name:           "night+sunday overlap, both eligible, additive",
			segments:       PremiumSegments{NightSundaySeconds: 4 * 3600},
			sundayEligible: true,
			nightEligible:  true,
			config: PremiumConfig{
				NightShiftMultiplier: 1.25,
				SundayMultiplier:     1.20,
				CumulationMode:       settingspkg.PremiumCumulationModeAdditive,
			},
			want: 5.8, // (1.25+1.20-1)=1.45 * 4h
		},
		{
			name:           "night+sunday overlap, both eligible, fixed with rate",
			segments:       PremiumSegments{NightSundaySeconds: 4 * 3600},
			sundayEligible: true,
			nightEligible:  true,
			config: PremiumConfig{
				NightShiftMultiplier:          1.25,
				SundayMultiplier:              1.20,
				CumulationMode:                settingspkg.PremiumCumulationModeFixed,
				NightSundayCombinedMultiplier: &fixedRate,
			},
			want: 6.0, // 1.5 * 4h
		},
		{
			name:           "night+sunday overlap, both eligible, fixed WITHOUT rate -> falls back to highest",
			segments:       PremiumSegments{NightSundaySeconds: 4 * 3600},
			sundayEligible: true,
			nightEligible:  true,
			config: PremiumConfig{
				NightShiftMultiplier: 1.25,
				SundayMultiplier:     1.20,
				CumulationMode:       settingspkg.PremiumCumulationModeFixed,
			},
			want: 5.0, // fallback max(1.25,1.20)=1.25 * 4h
		},
		{
			name:           "night+sunday overlap, only night eligible -> degrades to night rate",
			segments:       PremiumSegments{NightSundaySeconds: 4 * 3600},
			sundayEligible: false,
			nightEligible:  true,
			config: PremiumConfig{
				NightShiftMultiplier: 1.25,
				SundayMultiplier:     1.20,
				CumulationMode:       settingspkg.PremiumCumulationModeAdditive,
			},
			want: 5.0,
		},
		{
			name:           "night+sunday overlap, only sunday eligible -> degrades to sunday rate",
			segments:       PremiumSegments{NightSundaySeconds: 4 * 3600},
			sundayEligible: true,
			nightEligible:  false,
			config: PremiumConfig{
				NightShiftMultiplier: 1.25,
				SundayMultiplier:     1.20,
				CumulationMode:       settingspkg.PremiumCumulationModeAdditive,
			},
			want: 4.8,
		},
		{
			name:           "night+sunday overlap, neither eligible -> no premium",
			segments:       PremiumSegments{NightSundaySeconds: 4 * 3600},
			sundayEligible: false,
			nightEligible:  false,
			config:         baseConfig,
			want:           4.0,
		},
		{
			name:     "holiday marginal bucket is ignored by payroll weighting (deferred scope)",
			segments: PremiumSegments{NormalSeconds: 8 * 3600, HolidaySeconds: 8 * 3600},
			config:   baseConfig,
			want:     8.0,
		},
		{
			name:           "realistic mixed shift (normal+night+night_sunday)",
			segments:       PremiumSegments{NormalSeconds: 2 * 3600, NightSeconds: 2 * 3600, NightSundaySeconds: 2 * 3600},
			sundayEligible: true,
			nightEligible:  true,
			config:         baseConfig,
			want:           7.0, // 2*1 + 2*1.25 + 2*1.25
		},
		{
			name:           "zero segments, fixed mode with nil combined rate must not panic",
			segments:       PremiumSegments{},
			sundayEligible: true,
			nightEligible:  true,
			config: PremiumConfig{
				NightShiftMultiplier: 1.25,
				SundayMultiplier:     1.20,
				CumulationMode:       settingspkg.PremiumCumulationModeFixed,
			},
			want: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := weightedPremiumHours(tt.segments, tt.sundayEligible, tt.nightEligible, tt.config)
			if !almostEqual(got, tt.want) {
				t.Fatalf("weightedPremiumHours() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCombinedMultiplier(t *testing.T) {
	fixedRate := 1.5

	tests := []struct {
		name           string
		sundayEligible bool
		nightEligible  bool
		config         PremiumConfig
		want           float64
	}{
		{
			name:           "additive",
			sundayEligible: true,
			nightEligible:  true,
			config:         PremiumConfig{NightShiftMultiplier: 1.25, SundayMultiplier: 1.20, CumulationMode: settingspkg.PremiumCumulationModeAdditive},
			want:           1.45,
		},
		{
			name:           "highest",
			sundayEligible: true,
			nightEligible:  true,
			config:         PremiumConfig{NightShiftMultiplier: 1.25, SundayMultiplier: 1.20, CumulationMode: settingspkg.PremiumCumulationModeHighest},
			want:           1.25,
		},
		{
			name:           "fixed with rate",
			sundayEligible: true,
			nightEligible:  true,
			config: PremiumConfig{
				NightShiftMultiplier: 1.25, SundayMultiplier: 1.20,
				CumulationMode:                settingspkg.PremiumCumulationModeFixed,
				NightSundayCombinedMultiplier: &fixedRate,
			},
			want: 1.5,
		},
		{
			name:           "fixed without rate falls back to highest",
			sundayEligible: true,
			nightEligible:  true,
			config:         PremiumConfig{NightShiftMultiplier: 1.25, SundayMultiplier: 1.20, CumulationMode: settingspkg.PremiumCumulationModeFixed},
			want:           1.25,
		},
		{
			name:           "unrecognized mode falls back to highest",
			sundayEligible: true,
			nightEligible:  true,
			config:         PremiumConfig{NightShiftMultiplier: 1.25, SundayMultiplier: 1.20, CumulationMode: "bogus"},
			want:           1.25,
		},
		{
			name:           "only night eligible",
			sundayEligible: false,
			nightEligible:  true,
			config:         PremiumConfig{NightShiftMultiplier: 1.25, SundayMultiplier: 1.20, CumulationMode: settingspkg.PremiumCumulationModeAdditive},
			want:           1.25,
		},
		{
			name:           "only sunday eligible",
			sundayEligible: true,
			nightEligible:  false,
			config:         PremiumConfig{NightShiftMultiplier: 1.25, SundayMultiplier: 1.20, CumulationMode: settingspkg.PremiumCumulationModeAdditive},
			want:           1.20,
		},
		{
			name:           "neither eligible",
			sundayEligible: false,
			nightEligible:  false,
			config:         PremiumConfig{NightShiftMultiplier: 1.25, SundayMultiplier: 1.20, CumulationMode: settingspkg.PremiumCumulationModeAdditive},
			want:           1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := combinedMultiplier(tt.sundayEligible, tt.nightEligible, tt.config)
			if !almostEqual(got, tt.want) {
				t.Fatalf("combinedMultiplier() = %v, want %v", got, tt.want)
			}
		})
	}
}
