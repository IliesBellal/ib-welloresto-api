package analytics

import "sort"

// apportionCents rounds each fractional share to a whole number of cents
// while forcing the rounded values to sum to exactly total — the largest-
// remainder method (Hamilton's method): floor every share, then hand the
// leftover cents to the shares with the largest fractional remainder, one
// cent each.
//
// This exists because the previous approach — each SQL group independently
// summing its own lines and rounding once ("sum first, round once", still
// correct and used everywhere else in this package, e.g.
// GetRevenueTotalsHT/GetVATTotals) — cannot guarantee that a set of
// independently-rounded group totals sums to a separately, independently-
// rounded whole-period total. On a fiscal breakdown (TVA by_rate/by_channel)
// that mismatch reads as a bug, not a rounding footnote: see models.go's
// VATRateTotal/VATChannelTotal doc comments. apportionCents is only ever
// called with total = the already-computed, canonical period total (from
// GetVATTotals), so the breakdown always reconciles to the number displayed
// next to it, never to its own independently-rounded sum.
//
// shares must be non-negative (true for HT sums here — order lines don't
// carry negative prices). The order of the returned slice matches shares.
func apportionCents(total int64, shares []float64) []int64 {
	n := len(shares)
	if n == 0 {
		return nil
	}

	result := make([]int64, n)
	remainderOrder := make([]int, n)
	var flooredSum int64
	for i, s := range shares {
		floor := int64(s)
		if s < 0 {
			// Not expected for HT sums, but floor must round toward -inf, not
			// truncate toward zero, for the remainder math below to hold.
			floor--
		}
		result[i] = floor
		flooredSum += floor
		remainderOrder[i] = i
	}

	remainder := func(i int) float64 { return shares[i] - float64(result[i]) }
	sort.SliceStable(remainderOrder, func(a, b int) bool {
		return remainder(remainderOrder[a]) > remainder(remainderOrder[b])
	})

	leftover := total - flooredSum
	switch {
	case leftover > 0:
		for c := int64(0); c < leftover; c++ {
			result[remainderOrder[int(c)%n]]++
		}
	case leftover < 0:
		for c := int64(0); c < -leftover; c++ {
			result[remainderOrder[n-1-int(c)%n]]--
		}
	}

	return result
}
