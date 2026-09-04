package analytics

import "testing"

// TestOrdersPeriodTotals_CoversThreshold locks in the materiality threshold
// found while verifying accuracy against staging: merchant 212 (PROD, the
// biggest merchant in this system) has covers on 12 of 9,694 orders in a
// 12-month window (≈0.12%) — almost certainly demo/test noise, not real
// coverage. A bare ">0" covers check would render that noise as a
// precise-looking KPI; coversCoverageThreshold (20%) exists to mask it
// instead, without hiding an establishment that genuinely, if imperfectly,
// records covers.
func TestOrdersPeriodTotals_CoversThreshold(t *testing.T) {
	cases := []struct {
		name          string
		totals        OrdersTotals
		wantAvailable bool
	}{
		{
			name: "no orders at all",
			totals: OrdersTotals{
				OrderCount: 0,
			},
			wantAvailable: false,
		},
		{
			name: "sparse noise below threshold (12/9694, merchant 212's real staging ratio)",
			totals: OrdersTotals{
				OrderCount:                 9694,
				OrdersWithCovers:           12,
				TotalCovers:                22,
				TTCCentsOfOrdersWithCovers: 5000,
			},
			wantAvailable: false,
		},
		{
			name: "exactly at the 20% threshold",
			totals: OrdersTotals{
				OrderCount:                 10,
				OrdersWithCovers:           2,
				TotalCovers:                6,
				TTCCentsOfOrdersWithCovers: 2000,
			},
			wantAvailable: true,
		},
		{
			name: "well above threshold (disciplined POS entry)",
			totals: OrdersTotals{
				OrderCount:                 10,
				OrdersWithCovers:           9,
				TotalCovers:                27,
				TTCCentsOfOrdersWithCovers: 18000,
			},
			wantAvailable: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			period := ordersPeriodTotals("2026-01-01", "2026-01-31", tc.totals)
			if period.CoversDataAvailable != tc.wantAvailable {
				t.Fatalf("CoversDataAvailable = %v, want %v", period.CoversDataAvailable, tc.wantAvailable)
			}
			if tc.wantAvailable {
				if period.TotalCovers == nil || period.AvgBasketPerCoverCents == nil {
					t.Fatalf("expected non-nil covers fields when available, got %+v", period)
				}
			} else if period.TotalCovers != nil || period.AvgBasketPerCoverCents != nil {
				t.Fatalf("expected nil covers fields when unavailable, got %+v", period)
			}
		})
	}
}
