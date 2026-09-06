package analytics

import (
	"testing"
	"time"
)

func TestChannelFilter(t *testing.T) {
	t.Run("empty request defaults to all 8 channels, canonical order", func(t *testing.T) {
		got, ok := ChannelFilter(nil)
		if !ok {
			t.Fatalf("expected ok=true for empty request")
		}
		if len(got) != len(Channels) {
			t.Fatalf("expected %v, got %v", Channels, got)
		}
		for i := range Channels {
			if got[i] != Channels[i] {
				t.Fatalf("expected %v, got %v", Channels, got)
			}
		}
	})

	t.Run("a valid subset is preserved, deduped, canonical order", func(t *testing.T) {
		got, ok := ChannelFilter([]string{ChannelDelivery, ChannelDineIn, ChannelDelivery})
		if !ok {
			t.Fatalf("expected ok=true")
		}
		want := []string{ChannelDineIn, ChannelDelivery}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("an unknown value is rejected, not silently dropped", func(t *testing.T) {
		_, ok := ChannelFilter([]string{ChannelDineIn, "mobile"})
		if ok {
			t.Fatalf("expected ok=false for an unrecognized channel")
		}
	})
}

func TestComputeClientsSegments(t *testing.T) {
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC) // 12-month window

	t.Run("first order ever inside the window is nouveau, regardless of order count", func(t *testing.T) {
		rows := []CustomerLifetimeRow{
			{
				CustomerID:     "c1",
				FirstOrderDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				LastOrderDate:  time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
				LifetimeOrders: 6, // enough to clear the "fidele" bar, but first order is still inside the window
				PeriodOrders:   2,
			},
		}
		buckets := computeClientsSegments(rows, periodStart, periodEnd)
		if len(buckets[ClientsSegmentNew]) != 1 {
			t.Fatalf("expected the row in nouveau, got buckets=%v", buckets)
		}
		if len(buckets[ClientsSegmentLoyal]) != 0 {
			t.Fatalf("nouveau must take precedence over fidele even with >=5 lifetime orders")
		}
	})

	t.Run("restricting first-order to the window would wrongly call an old customer new — this must not happen", func(t *testing.T) {
		rows := []CustomerLifetimeRow{
			{
				CustomerID:     "c2",
				FirstOrderDate: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), // long before the window
				LastOrderDate:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
				LifetimeOrders: 3,
				PeriodOrders:   1,
			},
		}
		buckets := computeClientsSegments(rows, periodStart, periodEnd)
		if len(buckets[ClientsSegmentNew]) != 0 {
			t.Fatalf("a customer whose first order predates the window must never be nouveau")
		}
		if len(buckets[ClientsSegmentReturning]) != 1 {
			t.Fatalf("expected the row in recurrent (not new, not loyal, active in period), got buckets=%v", buckets)
		}
	})

	t.Run("fidele requires both the lifetime-order threshold and activity in the period", func(t *testing.T) {
		rows := []CustomerLifetimeRow{
			{
				CustomerID:     "c3",
				FirstOrderDate: time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC),
				LastOrderDate:  time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
				LifetimeOrders: 5,
				PeriodOrders:   1,
			},
		}
		buckets := computeClientsSegments(rows, periodStart, periodEnd)
		if len(buckets[ClientsSegmentLoyal]) != 1 {
			t.Fatalf("expected the row in fidele, got buckets=%v", buckets)
		}
	})

	t.Run("inactif: last order more than clientsInactivityDays before periodEnd", func(t *testing.T) {
		rows := []CustomerLifetimeRow{
			{
				CustomerID:     "c4",
				FirstOrderDate: time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC),
				LastOrderDate:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), // well over 180 days before periodEnd, and before periodStart
				LifetimeOrders: 10,
				PeriodOrders:   0,
			},
		}
		buckets := computeClientsSegments(rows, periodStart, periodEnd)
		if len(buckets[ClientsSegmentInactive]) != 1 {
			t.Fatalf("expected the row in inactif, got buckets=%v", buckets)
		}
	})

	// dormant only exists for a window SHORTER than clientsInactivityDays: if
	// periodEnd-periodStart >= clientsInactivityDays, any last order before
	// periodStart is already, by construction, more than clientsInactivityDays
	// before periodEnd — there is no gap left for "not active, not yet stale."
	// A 12-month window (used above) can never produce a dormant row; this
	// uses a short (30-day) window instead, where that gap is real.
	shortStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	shortEnd := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) // 30-day window, cutoff = shortEnd - 180d ≈ 2026-01-02

	t.Run("dormant: not active this period, but not stale enough to be inactif", func(t *testing.T) {
		rows := []CustomerLifetimeRow{
			{
				CustomerID:     "c5",
				FirstOrderDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				LastOrderDate:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), // before shortStart, but after the 180-day cutoff
				LifetimeOrders: 4,
				PeriodOrders:   0,
			},
		}
		buckets := computeClientsSegments(rows, shortStart, shortEnd)
		if len(buckets[ClientsSegmentDormant]) != 1 {
			t.Fatalf("expected the row in dormant (neither active nor stale enough), got buckets=%v", buckets)
		}
	})

	t.Run("every row lands in exactly one bucket — the partition is complete", func(t *testing.T) {
		rows := []CustomerLifetimeRow{
			{CustomerID: "c1", FirstOrderDate: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC), LastOrderDate: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC), LifetimeOrders: 1, PeriodOrders: 1},
			{CustomerID: "c2", FirstOrderDate: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), LastOrderDate: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC), LifetimeOrders: 3, PeriodOrders: 1},
			{CustomerID: "c3", FirstOrderDate: time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), LastOrderDate: time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC), LifetimeOrders: 5, PeriodOrders: 1},
			{CustomerID: "c4", FirstOrderDate: time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC), LastOrderDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), LifetimeOrders: 10, PeriodOrders: 0},
			{CustomerID: "c5", FirstOrderDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), LastOrderDate: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), LifetimeOrders: 4, PeriodOrders: 0},
		}
		buckets := computeClientsSegments(rows, shortStart, shortEnd)
		total := 0
		for _, seg := range clientsSegmentOrder {
			total += len(buckets[seg])
		}
		if total != len(rows) {
			t.Fatalf("expected every row classified exactly once (%d total), got %d across buckets=%v", len(rows), total, buckets)
		}
		if len(buckets[ClientsSegmentNew]) != 1 || len(buckets[ClientsSegmentReturning]) != 1 || len(buckets[ClientsSegmentLoyal]) != 1 ||
			len(buckets[ClientsSegmentInactive]) != 1 || len(buckets[ClientsSegmentDormant]) != 1 {
			t.Fatalf("expected exactly one row per bucket, got buckets=%v", buckets)
		}
	})
}

func TestClientsSegmentCounts(t *testing.T) {
	t.Run("a segment with zero period orders gets a nil avg basket, never a division by zero", func(t *testing.T) {
		buckets := map[string][]CustomerLifetimeRow{
			ClientsSegmentInactive: {{CustomerID: "c1", PeriodOrders: 0, PeriodRevenueCents: 0}},
		}
		counts := clientsSegmentCounts(buckets)
		for _, c := range counts {
			if c.Segment == ClientsSegmentInactive {
				if c.AvgBasketTTCCents != nil {
					t.Fatalf("expected nil avg basket for inactif, got %v", *c.AvgBasketTTCCents)
				}
				if c.Count != 1 {
					t.Fatalf("expected count 1, got %d", c.Count)
				}
			}
		}
	})

	t.Run("a segment with period orders gets the correct avg basket", func(t *testing.T) {
		buckets := map[string][]CustomerLifetimeRow{
			ClientsSegmentReturning: {
				{CustomerID: "c1", PeriodOrders: 2, PeriodRevenueCents: 3000},
				{CustomerID: "c2", PeriodOrders: 1, PeriodRevenueCents: 1000},
			},
		}
		counts := clientsSegmentCounts(buckets)
		for _, c := range counts {
			if c.Segment == ClientsSegmentReturning {
				if c.AvgBasketTTCCents == nil || *c.AvgBasketTTCCents != 1333 {
					t.Fatalf("expected avg basket 1333 (4000/3), got %v", c.AvgBasketTTCCents)
				}
				if c.Count != 2 {
					t.Fatalf("expected count 2, got %d", c.Count)
				}
			}
		}
	})
}
