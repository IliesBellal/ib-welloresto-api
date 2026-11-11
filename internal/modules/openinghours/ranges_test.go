package openinghours

import (
	"testing"
	"time"
)

func mustParis(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	return loc
}

func utc(t *testing.T, value string) time.Time {
	t.Helper()
	v, err := time.Parse("2006-01-02 15:04", value)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func assertRanges(t *testing.T, got []Range, want [][2]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d ranges %+v, want %d", len(got), got, len(want))
	}
	for i, w := range want {
		if !got[i].Start.Equal(utc(t, w[0])) || !got[i].End.Equal(utc(t, w[1])) {
			t.Fatalf("range %d = [%s, %s), want [%s, %s)", i,
				got[i].Start.Format(time.RFC3339), got[i].End.Format(time.RFC3339), w[0], w[1])
		}
	}
}

// Mercredi 11/11/2026 à Paris (UTC+1) : journée locale = 10/11 23:00 → 11/11 23:00 UTC.
func TestOpenRanges_LunchAndDinnerInMerchantTimezone(t *testing.T) {
	paris := mustParis(t)
	slots := []Slot{
		{DayOfWeekFrom: 1, DayOfWeekTo: 5, HourFrom: "12:00:00", HourTo: "14:30:00"},
		{DayOfWeekFrom: 1, DayOfWeekTo: 5, HourFrom: "19:00:00", HourTo: "22:30:00"},
	}

	got := OpenRanges(slots, utc(t, "2026-11-10 23:00"), utc(t, "2026-11-11 23:00"), paris)

	assertRanges(t, got, [][2]string{
		{"2026-11-11 11:00", "2026-11-11 13:30"},
		{"2026-11-11 18:00", "2026-11-11 21:30"},
	})
}

func TestOpenRanges_ClosedDayReturnsEmpty(t *testing.T) {
	paris := mustParis(t)
	// Ouvert du lundi au samedi : le dimanche 15/11/2026 est fermé.
	slots := []Slot{{DayOfWeekFrom: 1, DayOfWeekTo: 6, HourFrom: "12:00", HourTo: "14:00"}}

	got := OpenRanges(slots, utc(t, "2026-11-14 23:00"), utc(t, "2026-11-15 23:00"), paris)

	if got == nil || len(got) != 0 {
		t.Fatalf("expected an empty, non-nil slice, got %+v", got)
	}
}

func TestOpenRanges_MidnightSpanningSlotFromPreviousDay(t *testing.T) {
	// Mardi 19:00 → mercredi 01:00, vu depuis la journée du mercredi (UTC).
	slots := []Slot{{DayOfWeekFrom: 2, DayOfWeekTo: 2, HourFrom: "19:00:00", HourTo: "01:00:00"}}

	got := OpenRanges(slots, utc(t, "2026-11-11 00:00"), utc(t, "2026-11-12 00:00"), time.UTC)

	assertRanges(t, got, [][2]string{{"2026-11-11 00:00", "2026-11-11 01:00"}})
}

func TestOpenRanges_WrappingWeekAndMerge(t *testing.T) {
	// Samedi → lundi (bouclage), deux créneaux qui se touchent fusionnés.
	slots := []Slot{
		{DayOfWeekFrom: 6, DayOfWeekTo: 1, HourFrom: "10:00", HourTo: "15:00"},
		{DayOfWeekFrom: 7, DayOfWeekTo: 7, HourFrom: "15:00", HourTo: "18:00"},
	}

	// Dimanche 15/11/2026 en UTC.
	got := OpenRanges(slots, utc(t, "2026-11-15 00:00"), utc(t, "2026-11-16 00:00"), time.UTC)

	assertRanges(t, got, [][2]string{{"2026-11-15 10:00", "2026-11-15 18:00"}})
}
