package availabilities

import (
	"testing"
	"time"
)

func TestUnavailableProductsAt(t *testing.T) {
	// Lundi 2026-09-21, créneaux en heure locale (fuseau UTC ici pour isoler
	// la règle ; l'évaluation dans le fuseau du merchant est testée à part).
	monday := func(hh, mm int) time.Time { return time.Date(2026, 9, 21, hh, mm, 0, 0, time.UTC) }

	rows := []ProductScheduleRow{
		// Petit-déj : lundi 06:00-11:00.
		{ProductID: "1", ProductName: "Croissant", HasSchedule: true, DayOfWeek: 1, StartTime: "06:00:00", EndTime: "11:00:00"},
		// Deux créneaux le lundi : 11:00-14:00 et 18:00-22:00.
		{ProductID: "2", ProductName: "Plat", HasSchedule: true, DayOfWeek: 1, StartTime: "11:00:00", EndTime: "14:00:00"},
		{ProductID: "2", ProductName: "Plat", HasSchedule: true, DayOfWeek: 1, StartTime: "18:00:00", EndTime: "22:00:00"},
		// Disponibilité active sans créneau : jamais ouverte.
		{ProductID: "3", ProductName: "Fantome"},
		// Mardi uniquement.
		{ProductID: "4", ProductName: "Mardi", HasSchedule: true, DayOfWeek: 2, StartTime: "00:00:00", EndTime: "23:59:59"},
	}

	cases := []struct {
		name string
		at   time.Time
		want []string
	}{
		{"matin", monday(7, 0), []string{"2", "3", "4"}},
		{"borne de fin exclue", monday(11, 0), []string{"1", "3", "4"}},
		{"entre deux creneaux", monday(15, 0), []string{"1", "2", "3", "4"}},
		{"second creneau", monday(19, 0), []string{"1", "3", "4"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := UnavailableProductsAt(rows, tc.at, time.UTC)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want ids %v", got, tc.want)
			}
			for _, id := range tc.want {
				if _, ok := got[id]; !ok {
					t.Fatalf("expected %s unavailable, got %v", id, got)
				}
			}
		})
	}

	if got := UnavailableProductsAt(nil, monday(7, 0), time.UTC); len(got) != 0 {
		t.Fatalf("no availability must restrict nothing, got %v", got)
	}
}

func TestUnavailableProductsAt_EvaluatesInMerchantTimezone(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}
	// Petit-déj 06:00-11:00 heure de Paris, le lundi.
	rows := []ProductScheduleRow{
		{ProductID: "1", ProductName: "Croissant", HasSchedule: true, DayOfWeek: 1, StartTime: "06:00:00", EndTime: "11:00:00"},
	}

	cases := []struct {
		name      string
		at        time.Time
		available bool
	}{
		// Été (UTC+2) : 04:30 UTC = 06:30 à Paris.
		{"ete ouvert", time.Date(2026, 7, 6, 4, 30, 0, 0, time.UTC), true},
		// Été : 09:30 UTC = 11:30 à Paris, fermé.
		{"ete ferme", time.Date(2026, 7, 6, 9, 30, 0, 0, time.UTC), false},
		// Hiver (UTC+1) : même créneau mural, 05:30 UTC = 06:30 à Paris.
		{"hiver ouvert", time.Date(2026, 12, 7, 5, 30, 0, 0, time.UTC), true},
		// Hiver : 04:30 UTC = 05:30 à Paris, pas encore ouvert.
		{"hiver pas encore", time.Date(2026, 12, 7, 4, 30, 0, 0, time.UTC), false},
		// Dimanche 23:30 UTC = lundi 01:30 à Paris : le jour suit le fuseau local.
		{"jour local", time.Date(2026, 9, 20, 23, 30, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, blocked := UnavailableProductsAt(rows, tc.at, paris)["1"]
			if blocked == tc.available {
				t.Fatalf("at %s (Paris %s): available=%v, want %v", tc.at, tc.at.In(paris), !blocked, tc.available)
			}
		})
	}
}

func TestUnavailabilityFingerprint(t *testing.T) {
	if got := UnavailabilityFingerprint(nil); got != "all" {
		t.Fatalf("empty set fingerprint = %q, want all", got)
	}
	a := UnavailabilityFingerprint(map[string]string{"1": "a", "2": "b"})
	b := UnavailabilityFingerprint(map[string]string{"2": "x", "1": "y"})
	c := UnavailabilityFingerprint(map[string]string{"1": "a"})
	if a != b {
		t.Fatalf("fingerprint must depend on ids only, got %q vs %q", a, b)
	}
	if a == c {
		t.Fatalf("different sets must not share a fingerprint")
	}
}

func TestIsScheduleOpenAt_MidnightEnd(t *testing.T) {
	// Vendredi 19:00–00:00 (fin à minuit).
	friday := func(hh, mm int) time.Time { return time.Date(2026, 9, 25, hh, mm, 0, 0, time.UTC) }
	cases := []struct {
		at   time.Time
		want bool
	}{
		{friday(18, 59), false},
		{friday(19, 0), true},
		{friday(23, 59), true},
		{time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC), false}, // samedi 00:00 : plus le vendredi
	}
	for _, tc := range cases {
		if got := isScheduleOpenAt(5, "19:00:00", "00:00:00", tc.at); got != tc.want {
			t.Fatalf("at %s: got %v, want %v", tc.at, got, tc.want)
		}
	}
	if !isScheduleOpenAt(5, "00:00:00", "00:00:00", friday(12, 0)) {
		t.Fatalf("00:00–00:00 must cover the whole day")
	}
}

func TestValidateSchedules_SundayAndMidnight(t *testing.T) {
	schedules := []CreateAvailabilityScheduleReq{
		{DayOfWeek: 0, StartTime: "11:00", EndTime: "14:00"}, // dimanche, ancienne convention JS
		{DayOfWeek: 5, StartTime: "19:00", EndTime: "00:00"}, // jusqu'à minuit
	}
	if err := validateSchedules(schedules); err != nil {
		t.Fatalf("validateSchedules: %v", err)
	}
	if schedules[0].DayOfWeek != 7 {
		t.Fatalf("dimanche 0 must be stored as 7 (ISO), got %d", schedules[0].DayOfWeek)
	}

	if err := validateSchedules([]CreateAvailabilityScheduleReq{{DayOfWeek: 1, StartTime: "14:00", EndTime: "11:00"}}); err == nil {
		t.Fatal("reversed slot must be rejected")
	}
}
