package analytics

import "testing"

// TestStaffCancellationRateAvailable_Threshold locks in the "effectif"
// floor PROMPT 10 §4 requires before a per-server cancellation rate is
// shown — below it, the frontend must render raw counts only (see
// StaffCancellationRow's doc comment). Mirrors
// TestOrdersPeriodTotals_CoversThreshold's shape for the sibling gate in
// this same package.
func TestStaffCancellationRateAvailable_Threshold(t *testing.T) {
	cases := []struct {
		name          string
		ordersCreated int64
		want          bool
	}{
		{"zero orders", 0, false},
		{"a handful of orders (3, the brief's own noise example)", 3, false},
		{"just below the threshold", staffCancellationMinOrders - 1, false},
		{"exactly at the threshold", staffCancellationMinOrders, true},
		{"well above the threshold", staffCancellationMinOrders * 10, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := staffCancellationRateAvailable(tc.ordersCreated); got != tc.want {
				t.Fatalf("staffCancellationRateAvailable(%d) = %v, want %v", tc.ordersCreated, got, tc.want)
			}
		})
	}
}
