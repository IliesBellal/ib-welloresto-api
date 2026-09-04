package timeutil

import (
	"testing"
	"time"
)

// TestLocalDayBounds_DSTSpringForward covers PROMPT 03's explicit accuracy
// requirement ("le passage à l'heure d'été"): 2026-03-29 is when France
// springs forward (02:00 CET -> 03:00 CEST). The local calendar day is still
// exactly one day, but it spans only 23 hours of UTC — a bound built as
// "start + 24h" instead of the local midnight-to-midnight definition would
// silently include an hour of the next day.
func TestLocalDayBounds_DSTSpringForward(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	ref := time.Date(2026, 3, 29, 12, 0, 0, 0, loc)
	start, end := LocalDayBounds(ref, loc)

	wantStart := time.Date(2026, 3, 28, 23, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 3, 29, 22, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) {
		t.Fatalf("start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Fatalf("end = %v, want %v", end, wantEnd)
	}
	if got := end.Sub(start); got != 23*time.Hour {
		t.Fatalf("expected a 23h UTC span on spring-forward day, got %v", got)
	}
}

// TestLocalDayBounds_DSTFallBack covers the symmetric case: 2026-10-25,
// when France falls back (03:00 CEST -> 02:00 CET), spans 25 hours of UTC.
func TestLocalDayBounds_DSTFallBack(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	ref := time.Date(2026, 10, 25, 12, 0, 0, 0, loc)
	start, end := LocalDayBounds(ref, loc)

	wantStart := time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) {
		t.Fatalf("start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Fatalf("end = %v, want %v", end, wantEnd)
	}
	if got := end.Sub(start); got != 25*time.Hour {
		t.Fatalf("expected a 25h UTC span on fall-back day, got %v", got)
	}
}

// TestLocalDayBounds_MidnightOrder is the "commande créée à 00h30 heure de
// Paris" case from PROMPT 03: an order at 2026-01-15 00:30 Europe/Paris
// (winter, UTC+1) is 2026-01-14 23:30 UTC — a naive UTC-day bucketing would
// attribute it to Jan 14, not Jan 15.
func TestLocalDayBounds_MidnightOrder(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	orderTime := time.Date(2026, 1, 15, 0, 30, 0, 0, loc)
	dayStart, dayEnd := LocalDayBounds(orderTime, loc)

	orderUTC := orderTime.UTC()
	if orderUTC.Before(dayStart) || !orderUTC.Before(dayEnd) {
		t.Fatalf("order at %v (UTC %v) falls outside [%v, %v) — expected inside the Jan 15 local day", orderTime, orderUTC, dayStart, dayEnd)
	}

	// The previous local day (Jan 14) must NOT contain this order.
	prevStart, prevEnd := LocalDayBounds(orderTime.AddDate(0, 0, -1), loc)
	orderInPrevDay := !orderUTC.Before(prevStart) && orderUTC.Before(prevEnd)
	if orderInPrevDay {
		t.Fatalf("order at %v leaked into the Jan 14 bucket [%v, %v)", orderTime, prevStart, prevEnd)
	}
}

func TestLocalDayRangeBounds_MatchesPerDayBounds(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	from := time.Date(2026, 6, 1, 0, 0, 0, 0, loc)
	to := time.Date(2026, 6, 3, 0, 0, 0, 0, loc)

	rangeStart, rangeEnd := LocalDayRangeBounds(from, to, loc)
	dayOneStart, _ := LocalDayBounds(from, loc)
	_, dayThreeEnd := LocalDayBounds(to, loc)

	if !rangeStart.Equal(dayOneStart) {
		t.Fatalf("range start = %v, want %v", rangeStart, dayOneStart)
	}
	if !rangeEnd.Equal(dayThreeEnd) {
		t.Fatalf("range end = %v, want %v", rangeEnd, dayThreeEnd)
	}
}
