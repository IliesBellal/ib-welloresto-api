package helpers

import (
	"context"
	"testing"
	"time"
)

func TestMerchantLocation(t *testing.T) {
	ctx := context.Background()

	if loc := MerchantLocation(ctx, "Europe/Paris"); loc == nil || loc.String() != "Europe/Paris" {
		t.Fatalf("MerchantLocation(Europe/Paris) = %v", loc)
	}
	for _, name := range []string{"", "Paris", "Europe/Nowhere", " Europe/Paris "} {
		loc := MerchantLocation(ctx, name)
		if loc == nil {
			t.Fatalf("MerchantLocation(%q) must never be nil", name)
		}
		// time.Now().In(loc) ne doit jamais paniquer.
		_ = time.Now().In(loc)
	}
	if loc := MerchantLocation(ctx, "Europe/Nowhere"); loc != time.UTC {
		t.Fatalf("invalid timezone must fall back to UTC, got %v", loc)
	}
}

func TestISOWeekday(t *testing.T) {
	monday := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	for i, want := range []int{1, 2, 3, 4, 5, 6, 7} {
		if got := ISOWeekday(monday.AddDate(0, 0, i)); got != want {
			t.Fatalf("ISOWeekday(%s) = %d, want %d", monday.AddDate(0, 0, i).Weekday(), got, want)
		}
	}
}
