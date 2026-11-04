package deliveroo

import (
	"testing"
	"time"
)

func paris(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skipf("fuseau indisponible: %v", err)
	}
	return loc
}

var lunchDinner = OpeningHours{
	"monday":  {{LocalStartTime: "12:00", LocalEndTime: "14:00"}, {LocalStartTime: "19:00", LocalEndTime: "22:00"}},
	"tuesday": {{LocalStartTime: "12:00", LocalEndTime: "14:00"}},
}

func TestNextOpeningWake_PicksNextPeriodInSiteTimezone(t *testing.T) {
	loc := paris(t)
	// Lundi 2026-11-02 11:00 Paris : prochaine ouverture 12:00 -> réveil 12:05.
	now := time.Date(2026, 11, 2, 11, 0, 0, 0, loc)
	got, ok := NextOpeningWake(now, loc, lunchDinner)
	if !ok || !got.Equal(time.Date(2026, 11, 2, 12, 5, 0, 0, loc)) {
		t.Fatalf("got %v ok=%v, want 2026-11-02 12:05 Paris", got, ok)
	}

	// 12:05 pile : strictement après -> la plage du soir.
	got, _ = NextOpeningWake(time.Date(2026, 11, 2, 12, 5, 0, 0, loc), loc, lunchDinner)
	if !got.Equal(time.Date(2026, 11, 2, 19, 5, 0, 0, loc)) {
		t.Fatalf("got %v, want 19:05", got)
	}

	// Après le dîner de lundi : mardi 12:05.
	got, _ = NextOpeningWake(time.Date(2026, 11, 2, 23, 0, 0, 0, loc), loc, lunchDinner)
	if !got.Equal(time.Date(2026, 11, 3, 12, 5, 0, 0, loc)) {
		t.Fatalf("got %v, want mardi 12:05", got)
	}
}

func TestNextOpeningWake_SkipsClosedDaysAndWrapsTheWeek(t *testing.T) {
	loc := paris(t)
	// Mardi soir : mercredi à dimanche fermés, retour lundi 12:05.
	got, ok := NextOpeningWake(time.Date(2026, 11, 3, 20, 0, 0, 0, loc), loc, lunchDinner)
	if !ok || !got.Equal(time.Date(2026, 11, 9, 12, 5, 0, 0, loc)) {
		t.Fatalf("got %v ok=%v, want lundi 2026-11-09 12:05", got, ok)
	}
}

func TestNextOpeningWake_UsesSiteTimezoneNotServerOne(t *testing.T) {
	paris := paris(t)
	// 10:30 UTC = 11:30 Paris : l'ouverture de 12:00 Paris est encore à venir.
	utc := time.Date(2026, 11, 2, 10, 30, 0, 0, time.UTC)
	got, _ := NextOpeningWake(utc, paris, lunchDinner)
	if !got.Equal(time.Date(2026, 11, 2, 12, 5, 0, 0, paris)) {
		t.Fatalf("got %v, want 12:05 Paris", got)
	}
}

func TestNextOpeningWake_DSTChange(t *testing.T) {
	loc := paris(t)
	// Passage à l'heure d'hiver le dimanche 2026-10-25 : lundi 26, 12:00 reste 12:00 local.
	got, _ := NextOpeningWake(time.Date(2026, 10, 25, 8, 0, 0, 0, loc), loc, lunchDinner)
	want := time.Date(2026, 10, 26, 12, 5, 0, 0, loc)
	if !got.Equal(want) || got.Hour() != 12 {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNextOpeningWake_NoUsableHours(t *testing.T) {
	loc := paris(t)
	now := time.Now()
	if _, ok := NextOpeningWake(now, loc, nil); ok {
		t.Error("horaires absents : aucun réveil")
	}
	if _, ok := NextOpeningWake(now, nil, lunchDinner); ok {
		t.Error("fuseau absent : aucun réveil")
	}
	bad := OpeningHours{"monday": {{LocalStartTime: "n'importe quoi"}}}
	if _, ok := NextOpeningWake(now, loc, bad); ok {
		t.Error("heure illisible : aucun réveil")
	}
}

func TestParseLocalClock(t *testing.T) {
	cases := map[string][2]int{"12:00": {12, 0}, "09:30:00": {9, 30}, "2020-11-24 18:45": {18, 45}, "2020-11-24T07:05:00": {7, 5}}
	for in, want := range cases {
		h, m, ok := parseLocalClock(in)
		if !ok || h != want[0] || m != want[1] {
			t.Errorf("parseLocalClock(%q) = %d:%d ok=%v, want %d:%d", in, h, m, ok, want[0], want[1])
		}
	}
	for _, in := range []string{"", "25:00", "12:99", "abc"} {
		if _, _, ok := parseLocalClock(in); ok {
			t.Errorf("parseLocalClock(%q) devrait échouer", in)
		}
	}
}
