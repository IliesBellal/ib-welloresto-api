package analytics

import "testing"

// TestUpsellTransformationRateAvailable_Threshold locks in the materiality
// floor before UpsellSuggestionsTotals' transformation rate is shown — below
// it, the frontend must render ProposedCount/AcceptedCount only (see
// UpsellSuggestionsTotals' doc comment, models.go). Mirrors
// TestStaffCancellationRateAvailable_Threshold's shape for the sibling gate
// in this same package.
func TestUpsellTransformationRateAvailable_Threshold(t *testing.T) {
	cases := []struct {
		name     string
		proposed int64
		want     bool
	}{
		{"zero suggestions", 0, false},
		{"a handful of suggestions (1, staging's real current volume)", 1, false},
		{"just below the threshold", upsellSuggestionsMinProposed - 1, false},
		{"exactly at the threshold", upsellSuggestionsMinProposed, true},
		{"well above the threshold (287, staging's test establishment)", 287, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := upsellTransformationRateAvailable(tc.proposed); got != tc.want {
				t.Fatalf("upsellTransformationRateAvailable(%d) = %v, want %v", tc.proposed, got, tc.want)
			}
		})
	}
}
