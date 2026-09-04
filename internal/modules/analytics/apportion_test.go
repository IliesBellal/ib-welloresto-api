package analytics

import "testing"

// TestApportionCents_SumsToTotal is the property the fix in PROMPT 06 §1
// exists to guarantee: whatever the shares, the returned parts always sum to
// exactly total. Run across the fixture that exposed the original bug
// (postgres_integration_orders_payments_vat_test.go's by_rate breakdown,
// 2083.333.../1818.181... against a period total of 3902) plus edge cases.
func TestApportionCents_SumsToTotal(t *testing.T) {
	cases := []struct {
		name   string
		total  int64
		shares []float64
	}{
		{"vat by_rate fixture (rate20+rate10)", 3902, []float64{2083.3334, 1818.1818}},
		{"vat by_channel fixture (dine_in/takeaway/delivery)", 3902, []float64{1666.6667, 416.6667, 1818.1818}},
		{"exact shares, no rounding needed", 100, []float64{50, 30, 20}},
		{"single share", 3902, []float64{3902.4}},
		{"empty period", 0, []float64{}},
		{"all zero shares, nonzero total impossible in practice but must not panic", 0, []float64{0, 0, 0}},
		{"many groups, leftover larger than a single pass would assume", 10, []float64{0.9, 0.9, 0.9, 0.9, 0.9, 0.9, 0.9, 0.9, 0.9, 0.9}},
		{"total below floored sum (negative leftover)", 3900, []float64{2083.3334, 1818.1818}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parts := apportionCents(tc.total, tc.shares)
			if len(parts) != len(tc.shares) {
				t.Fatalf("expected %d parts, got %d", len(tc.shares), len(parts))
			}
			var sum int64
			for _, p := range parts {
				sum += p
			}
			if sum != tc.total {
				t.Fatalf("parts %v sum to %d, want %d", parts, sum, tc.total)
			}
		})
	}
}

// TestApportionCents_LargestRemainderFirst locks in which group absorbs the
// rounding: the one with the largest fractional part, matching the
// docs/analytics/MESURES.md-documented example (rate20 share 2083.3334 beats
// rate10 share 1818.1818's remainder, so rate20 — not rate10 — gets the extra
// cent).
func TestApportionCents_LargestRemainderFirst(t *testing.T) {
	parts := apportionCents(3902, []float64{2083.3334, 1818.1818})
	if parts[0] != 2084 {
		t.Fatalf("expected rate20 (largest remainder, .3334) to get the leftover cent -> 2084, got %d", parts[0])
	}
	if parts[1] != 1818 {
		t.Fatalf("expected rate10 to stay at its floor, 1818, got %d", parts[1])
	}
}

func TestApportionCents_EmptyShares(t *testing.T) {
	if got := apportionCents(0, nil); got != nil {
		t.Fatalf("expected nil for empty shares, got %v", got)
	}
}
