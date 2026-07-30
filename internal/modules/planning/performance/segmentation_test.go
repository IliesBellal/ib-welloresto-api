package performance

import (
	"testing"
	"time"
)

// nextWeekday returns the next date >= from (inclusive) that falls on the
// given weekday, so tests never rely on memorized calendar facts.
func nextWeekday(from time.Time, weekday time.Weekday) time.Time {
	for from.Weekday() != weekday {
		from = from.AddDate(0, 0, 1)
	}
	return from
}

func mustNightWindow(t *testing.T, start, end string) nightWindow {
	t.Helper()
	night, err := parseNightWindow(start, end)
	if err != nil {
		t.Fatalf("parseNightWindow(%q, %q) error: %v", start, end, err)
	}
	return night
}

func TestSegmentInterval_PlainDayShiftNoOverlap(t *testing.T) {
	night := mustNightWindow(t, "22:00:00", "06:00:00")
	saturday := nextWeekday(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Saturday)

	start := time.Date(saturday.Year(), saturday.Month(), saturday.Day(), 9, 0, 0, 0, time.UTC)
	end := start.Add(8 * time.Hour)

	got := segmentInterval(start, end, night, nil)
	want := PremiumSegments{NormalSeconds: 8 * 3600}
	if got != want {
		t.Fatalf("segmentInterval() = %+v, want %+v", got, want)
	}
}

func TestSegmentInterval_OvernightShiftCrossesIntoSunday(t *testing.T) {
	night := mustNightWindow(t, "22:00:00", "06:00:00")
	saturday := nextWeekday(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Saturday)

	// Saturday 20:00 -> Sunday 02:00 (6h).
	start := time.Date(saturday.Year(), saturday.Month(), saturday.Day(), 20, 0, 0, 0, time.UTC)
	end := start.Add(6 * time.Hour)

	got := segmentInterval(start, end, night, nil)
	// Sat 20:00-22:00 (normal) + Sat 22:00-24:00 (night) + Sun 00:00-02:00 (night+sunday).
	want := PremiumSegments{
		NormalSeconds:      2 * 3600,
		NightSeconds:       2 * 3600,
		NightSundaySeconds: 2 * 3600,
	}
	if got != want {
		t.Fatalf("segmentInterval() = %+v, want %+v", got, want)
	}
}

func TestSegmentInterval_SundayDaytimeNoNight(t *testing.T) {
	night := mustNightWindow(t, "22:00:00", "06:00:00")
	sunday := nextWeekday(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Sunday)

	start := time.Date(sunday.Year(), sunday.Month(), sunday.Day(), 10, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Hour)

	got := segmentInterval(start, end, night, nil)
	want := PremiumSegments{SundaySeconds: 4 * 3600}
	if got != want {
		t.Fatalf("segmentInterval() = %+v, want %+v", got, want)
	}
}

func TestSegmentInterval_HolidayIsMarginalToNormal(t *testing.T) {
	night := mustNightWindow(t, "22:00:00", "06:00:00")
	weekday := nextWeekday(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Wednesday)

	start := time.Date(weekday.Year(), weekday.Month(), weekday.Day(), 9, 0, 0, 0, time.UTC)
	end := start.Add(8 * time.Hour)
	holidays := map[string]bool{start.Format("2006-01-02"): true}

	got := segmentInterval(start, end, night, holidays)
	want := PremiumSegments{NormalSeconds: 8 * 3600, HolidaySeconds: 8 * 3600}
	if got != want {
		t.Fatalf("segmentInterval() = %+v, want %+v", got, want)
	}
}

func TestSegmentInterval_NightWindowNotCrossingMidnight(t *testing.T) {
	// Unusual config (01:00-05:00) but must still be handled correctly.
	night := mustNightWindow(t, "01:00:00", "05:00:00")
	weekday := nextWeekday(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Tuesday)

	start := time.Date(weekday.Year(), weekday.Month(), weekday.Day(), 0, 0, 0, 0, time.UTC)
	end := start.Add(6 * time.Hour)

	got := segmentInterval(start, end, night, nil)
	want := PremiumSegments{NormalSeconds: 2 * 3600, NightSeconds: 4 * 3600}
	if got != want {
		t.Fatalf("segmentInterval() = %+v, want %+v", got, want)
	}
}

func TestSegmentInterval_ZeroOrNegativeDuration(t *testing.T) {
	night := mustNightWindow(t, "22:00:00", "06:00:00")
	start := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

	if got := segmentInterval(start, start, night, nil); got != (PremiumSegments{}) {
		t.Fatalf("zero-duration segmentInterval() = %+v, want zero value", got)
	}
	if got := segmentInterval(start, start.Add(-time.Hour), night, nil); got != (PremiumSegments{}) {
		t.Fatalf("negative-duration segmentInterval() = %+v, want zero value", got)
	}
}

func TestPremiumSegments_ApplyBreakProration(t *testing.T) {
	segments := PremiumSegments{NormalSeconds: 4 * 3600, NightSeconds: 2 * 3600}

	got := segments.applyBreakProration(30 * 60)
	want := PremiumSegments{NormalSeconds: 13200, NightSeconds: 6600}
	if got != want {
		t.Fatalf("applyBreakProration(30min) = %+v, want %+v", got, want)
	}

	grossTotal := want.NormalSeconds + want.NightSeconds
	if grossTotal != 4*3600+2*3600-30*60 {
		t.Fatalf("prorated total = %d, want gross minus break", grossTotal)
	}
}

func TestPremiumSegments_ApplyBreakProration_NoOp(t *testing.T) {
	segments := PremiumSegments{NormalSeconds: 4 * 3600}

	if got := segments.applyBreakProration(0); got != segments {
		t.Fatalf("applyBreakProration(0) = %+v, want unchanged %+v", got, segments)
	}
}

func TestPremiumSegments_ApplyBreakProration_ClampsAtZero(t *testing.T) {
	segments := PremiumSegments{NormalSeconds: 3600}

	got := segments.applyBreakProration(2 * 3600)
	if got.NormalSeconds < 0 {
		t.Fatalf("applyBreakProration overshoot = %+v, must not go negative", got)
	}
	if got.NormalSeconds != 0 {
		t.Fatalf("applyBreakProration(break > gross) = %+v, want NormalSeconds=0", got)
	}
}

func TestPremiumSegments_ApplyBreakProration_HolidayMarginalReduced(t *testing.T) {
	segments := PremiumSegments{NormalSeconds: 8 * 3600, HolidaySeconds: 8 * 3600}

	got := segments.applyBreakProration(3600)
	if got.NormalSeconds != 7*3600 {
		t.Fatalf("NormalSeconds = %d, want %d", got.NormalSeconds, 7*3600)
	}
	if got.HolidaySeconds != 7*3600 {
		t.Fatalf("HolidaySeconds = %d, want %d (marginal, reduced proportionally)", got.HolidaySeconds, 7*3600)
	}
}
