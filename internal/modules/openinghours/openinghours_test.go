package openinghours

import (
	"testing"
	"time"
)

// Semaine de référence : lundi 2026-07-13 ... dimanche 2026-07-19.
func at(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(DateTimeLayout, value)
	if err != nil {
		t.Fatalf("invalid test datetime %q: %v", value, err)
	}
	return parsed
}

func assertBound(t *testing.T, name string, got *time.Time, want string) {
	t.Helper()
	if FormatDateTime(got) != want {
		t.Errorf("%s = %q, want %q", name, FormatDateTime(got), want)
	}
}

func TestComputePOSStatus_NoSlots(t *testing.T) {
	status := ComputePOSStatus(at(t, "2026-07-15 12:00:00"), nil)

	if status.IsOpen {
		t.Error("IsOpen = true, want false")
	}
	for name, b := range map[string]*time.Time{
		"LastStart": status.LastStart, "LastEnd": status.LastEnd,
		"CurrentStart": status.CurrentStart, "CurrentEnd": status.CurrentEnd,
		"NextStart": status.NextStart, "NextEnd": status.NextEnd,
	} {
		if b != nil {
			t.Errorf("%s = %v, want nil", name, b)
		}
	}
}

func TestComputePOSStatus_OpenNow(t *testing.T) {
	slots := []Slot{
		{DayOfWeekFrom: 1, DayOfWeekTo: 1, HourFrom: "09:00:00", HourTo: "14:00:00"}, // lundi
		{DayOfWeekFrom: 3, DayOfWeekTo: 3, HourFrom: "09:00:00", HourTo: "14:00:00"}, // mercredi midi
		{DayOfWeekFrom: 3, DayOfWeekTo: 3, HourFrom: "18:00:00", HourTo: "23:00:00"}, // mercredi soir
	}
	// Mercredi 12:00 : service du midi en cours.
	status := ComputePOSStatus(at(t, "2026-07-15 12:00:00"), slots)

	if !status.IsOpen {
		t.Fatal("IsOpen = false, want true")
	}
	assertBound(t, "CurrentStart", status.CurrentStart, "2026-07-15 09:00:00")
	assertBound(t, "CurrentEnd", status.CurrentEnd, "2026-07-15 14:00:00")
	assertBound(t, "LastStart", status.LastStart, "2026-07-13 09:00:00")
	assertBound(t, "LastEnd", status.LastEnd, "2026-07-13 14:00:00")
	assertBound(t, "NextStart", status.NextStart, "2026-07-15 18:00:00")
	assertBound(t, "NextEnd", status.NextEnd, "2026-07-15 23:00:00")
}

func TestComputePOSStatus_ClosedBeforeOpening(t *testing.T) {
	slots := []Slot{
		{DayOfWeekFrom: 1, DayOfWeekTo: 1, HourFrom: "09:00:00", HourTo: "14:00:00"},
		{DayOfWeekFrom: 3, DayOfWeekTo: 3, HourFrom: "09:00:00", HourTo: "14:00:00"},
	}
	// Mercredi 08:00 : fermé, ouverture dans une heure.
	status := ComputePOSStatus(at(t, "2026-07-15 08:00:00"), slots)

	if status.IsOpen {
		t.Fatal("IsOpen = true, want false")
	}
	assertBound(t, "CurrentStart", status.CurrentStart, "")
	assertBound(t, "NextStart", status.NextStart, "2026-07-15 09:00:00")
	assertBound(t, "NextEnd", status.NextEnd, "2026-07-15 14:00:00")
	assertBound(t, "LastStart", status.LastStart, "2026-07-13 09:00:00")
}

func TestComputePOSStatus_ClosedAfterClosing(t *testing.T) {
	slots := []Slot{
		{DayOfWeekFrom: 3, DayOfWeekTo: 3, HourFrom: "18:00:00", HourTo: "23:00:00"},
		{DayOfWeekFrom: 5, DayOfWeekTo: 5, HourFrom: "09:00:00", HourTo: "14:00:00"},
	}
	// Mercredi 23:30 : le service du soir vient de finir.
	status := ComputePOSStatus(at(t, "2026-07-15 23:30:00"), slots)

	if status.IsOpen {
		t.Fatal("IsOpen = true, want false")
	}
	assertBound(t, "LastStart", status.LastStart, "2026-07-15 18:00:00")
	assertBound(t, "LastEnd", status.LastEnd, "2026-07-15 23:00:00")
	assertBound(t, "NextStart", status.NextStart, "2026-07-17 09:00:00")
	assertBound(t, "NextEnd", status.NextEnd, "2026-07-17 14:00:00")
}

// BETWEEN MySQL est inclusif des deux côtés : ouvert pile à l'ouverture et
// pile à la fermeture.
func TestComputePOSStatus_BoundariesInclusive(t *testing.T) {
	slots := []Slot{{DayOfWeekFrom: 3, DayOfWeekTo: 3, HourFrom: "09:00:00", HourTo: "14:00:00"}}

	for _, moment := range []string{"2026-07-15 09:00:00", "2026-07-15 14:00:00"} {
		if !ComputePOSStatus(at(t, moment), slots).IsOpen {
			t.Errorf("IsOpen at %s = false, want true", moment)
		}
	}
	if ComputePOSStatus(at(t, "2026-07-15 14:00:01"), slots).IsOpen {
		t.Error("IsOpen one second after closing = true, want false")
	}
}

// Un créneau à cheval sur minuit (hour_from > hour_to) ne matche jamais comme
// créneau courant — parité avec le BETWEEN de la procédure MySQL. Ces créneaux
// doivent être saisis en deux lignes (22:00-23:59:59 + 00:00-02:00).
func TestComputePOSStatus_MidnightSpanningSlotNeverCurrent(t *testing.T) {
	slots := []Slot{{DayOfWeekFrom: 6, DayOfWeekTo: 6, HourFrom: "22:00:00", HourTo: "02:00:00"}}

	// Samedi 23:00 (dans le créneau réel) et samedi 01:00 (idem).
	for _, moment := range []string{"2026-07-18 23:00:00", "2026-07-18 01:00:00"} {
		if ComputePOSStatus(at(t, moment), slots).IsOpen {
			t.Errorf("IsOpen at %s = true, want false (MySQL BETWEEN parity)", moment)
		}
	}
}

// time.Weekday() donne dimanche = 0 ; la table utilise dimanche = 7.
func TestComputePOSStatus_SundayMapping(t *testing.T) {
	slots := []Slot{{DayOfWeekFrom: 7, DayOfWeekTo: 7, HourFrom: "10:00:00", HourTo: "15:00:00"}}

	status := ComputePOSStatus(at(t, "2026-07-19 12:00:00"), slots)
	if !status.IsOpen {
		t.Fatal("IsOpen sunday noon = false, want true")
	}
	assertBound(t, "CurrentStart", status.CurrentStart, "2026-07-19 10:00:00")
}

// La procédure raisonne dans la semaine courante : un lundi, le créneau du
// dimanche précédent n'est pas "dernier créneau" (day > day_of_week_to est
// faux), il redevient "prochain créneau" (dimanche suivant). Comportement
// conservé à l'identique.
func TestComputePOSStatus_MondayIgnoresPreviousSunday(t *testing.T) {
	slots := []Slot{{DayOfWeekFrom: 7, DayOfWeekTo: 7, HourFrom: "10:00:00", HourTo: "15:00:00"}}

	status := ComputePOSStatus(at(t, "2026-07-13 08:00:00"), slots)
	if status.IsOpen {
		t.Fatal("IsOpen = true, want false")
	}
	assertBound(t, "LastStart", status.LastStart, "")
	assertBound(t, "NextStart", status.NextStart, "2026-07-19 10:00:00")
}

// Créneau multi-jours (lundi-vendredi) : les bornes du créneau courant sont
// datées du jour courant (comportement historique de la procédure), et le
// dernier créneau après la fin de la plage couvre toute la plage.
func TestComputePOSStatus_MultiDayRange(t *testing.T) {
	slots := []Slot{{DayOfWeekFrom: 1, DayOfWeekTo: 5, HourFrom: "09:00:00", HourTo: "18:00:00"}}

	// Mercredi 12:00 : ouvert, bornes datées du jour même.
	open := ComputePOSStatus(at(t, "2026-07-15 12:00:00"), slots)
	if !open.IsOpen {
		t.Fatal("IsOpen wednesday noon = false, want true")
	}
	assertBound(t, "CurrentStart", open.CurrentStart, "2026-07-15 09:00:00")
	assertBound(t, "CurrentEnd", open.CurrentEnd, "2026-07-15 18:00:00")
	assertBound(t, "NextStart", open.NextStart, "")
	assertBound(t, "LastStart", open.LastStart, "")

	// Samedi 12:00 : la plage est passée, bornes lundi -> vendredi.
	closed := ComputePOSStatus(at(t, "2026-07-18 12:00:00"), slots)
	if closed.IsOpen {
		t.Fatal("IsOpen saturday = true, want false")
	}
	assertBound(t, "LastStart", closed.LastStart, "2026-07-13 09:00:00")
	assertBound(t, "LastEnd", closed.LastEnd, "2026-07-17 18:00:00")
}

// ORDER BY day_of_week_from, hour_from LIMIT 1 : le prochain créneau est le
// plus tôt dans la semaine ; le dernier est le plus tard.
func TestComputePOSStatus_PicksClosestSlots(t *testing.T) {
	slots := []Slot{
		{DayOfWeekFrom: 1, DayOfWeekTo: 1, HourFrom: "09:00:00", HourTo: "14:00:00"},
		{DayOfWeekFrom: 3, DayOfWeekTo: 3, HourFrom: "09:00:00", HourTo: "14:00:00"},
		{DayOfWeekFrom: 3, DayOfWeekTo: 3, HourFrom: "16:00:00", HourTo: "20:00:00"},
		{DayOfWeekFrom: 6, DayOfWeekTo: 6, HourFrom: "10:00:00", HourTo: "12:00:00"},
	}
	// Vendredi 08:00 : dernier = mercredi soir, prochain = samedi matin.
	status := ComputePOSStatus(at(t, "2026-07-17 08:00:00"), slots)

	assertBound(t, "LastStart", status.LastStart, "2026-07-15 16:00:00")
	assertBound(t, "LastEnd", status.LastEnd, "2026-07-15 20:00:00")
	assertBound(t, "NextStart", status.NextStart, "2026-07-18 10:00:00")
	assertBound(t, "NextEnd", status.NextEnd, "2026-07-18 12:00:00")
}

func TestComputePOSStatus_HourMinuteFormat(t *testing.T) {
	slots := []Slot{{DayOfWeekFrom: 3, DayOfWeekTo: 3, HourFrom: "09:00", HourTo: "14:00"}}

	if !ComputePOSStatus(at(t, "2026-07-15 12:00:00"), slots).IsOpen {
		t.Error("IsOpen with HH:MM hours = false, want true")
	}
}

func TestComputePOSStatus_MalformedHoursIgnored(t *testing.T) {
	slots := []Slot{
		{DayOfWeekFrom: 3, DayOfWeekTo: 3, HourFrom: "not-a-time", HourTo: "14:00:00"},
		{DayOfWeekFrom: 3, DayOfWeekTo: 3, HourFrom: "09:00:00", HourTo: "14:00:00"},
	}
	status := ComputePOSStatus(at(t, "2026-07-15 12:00:00"), slots)

	if !status.IsOpen {
		t.Fatal("valid slot must still match")
	}
	assertBound(t, "CurrentStart", status.CurrentStart, "2026-07-15 09:00:00")
}
