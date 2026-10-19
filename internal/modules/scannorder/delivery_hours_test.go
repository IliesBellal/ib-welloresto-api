package scannorder

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"welloresto-api/internal/models"
)

// paris : les créneaux sont comparés à l'heure locale du marchand.
var paris = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return time.FixedZone("CET", 3600)
	}
	return loc
}()

// 2026-10-20 est un mardi.
func at(day int, hhmm string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02 15:04", "2026-10-"+map[int]string{
		1: "19", 2: "20", 3: "21", 4: "22", 5: "23", 6: "24", 7: "25",
	}[day]+" "+hhmm, paris)
	return t
}

// Exemple de référence : livraison le soir 19h-22h30 tous les jours, le midi
// (11h30-14h) uniquement le mardi.
func eveningPlusTuesdayLunch() []DeliveryHour {
	hours := []DeliveryHour{{DayOfWeek: 2, HourFrom: "11:30", HourTo: "14:00"}}
	for day := 1; day <= 7; day++ {
		hours = append(hours, DeliveryHour{DayOfWeek: day, HourFrom: "19:00", HourTo: "22:30"})
	}
	return hours
}

func TestIsDeliveryOpenAt_NoHoursMeansNoRestriction(t *testing.T) {
	if !isDeliveryOpenAt(nil, at(1, "03:00")) {
		t.Fatal("sans créneau configuré, la livraison doit suivre les horaires d'ouverture (aucune restriction)")
	}
}

func TestIsDeliveryOpenAt_Slots(t *testing.T) {
	hours := eveningPlusTuesdayLunch()
	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"mardi midi", at(2, "12:15"), true},
		{"lundi midi : pas de livraison", at(1, "12:15"), false},
		{"mercredi midi : pas de livraison", at(3, "12:15"), false},
		{"début inclus", at(4, "19:00"), true},
		{"fin exclue", at(4, "22:30"), false},
		{"établissement ouvert mais avant le créneau", at(5, "18:30"), false},
		{"établissement ouvert mais après le créneau", at(6, "23:30"), false},
		{"dimanche soir", at(7, "21:00"), true},
	}
	for _, c := range cases {
		if got := isDeliveryOpenAt(hours, c.at); got != c.want {
			t.Errorf("%s : got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIsDeliveryOpenAt_SlotCrossingMidnight(t *testing.T) {
	// Vendredi 22h -> 1h du matin (samedi).
	hours := []DeliveryHour{{DayOfWeek: 5, HourFrom: "22:00", HourTo: "01:00"}}
	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"vendredi 23h", at(5, "23:00"), true},
		{"samedi 0h30", at(6, "00:30"), true},
		{"samedi 1h (fin exclue)", at(6, "01:00"), false},
		{"vendredi 0h30 (relève du jeudi)", at(5, "00:30"), false},
		{"vendredi 21h", at(5, "21:00"), false},
	}
	for _, c := range cases {
		if got := isDeliveryOpenAt(hours, c.at); got != c.want {
			t.Errorf("%s : got %v, want %v", c.name, got, c.want)
		}
	}

	// Dimanche -> lundi : le lendemain du jour 7 est le jour 1.
	sunday := []DeliveryHour{{DayOfWeek: 7, HourFrom: "19:00", HourTo: "00:30"}}
	if !isDeliveryOpenAt(sunday, at(1, "00:15")) {
		t.Error("lundi 0h15 doit relever du créneau du dimanche soir")
	}
}

func TestDeliveryRequestedAt(t *testing.T) {
	now := at(1, "12:00")

	immediate := &models.OrderRequest{EstimatedReady: "2026-10-19T10:20:00Z", IsScheduled: false}
	if got := deliveryRequestedAt(immediate, now); !got.Equal(now) {
		t.Errorf("commande immédiate : got %v, want maintenant (%v) — estimated_ready n'est qu'une estimation de préparation", got, now)
	}

	scheduled := &models.OrderRequest{EstimatedReady: "2026-10-19T18:00:00Z", IsScheduled: true}
	got := deliveryRequestedAt(scheduled, now)
	if got.Format("15:04") != "20:00" || got.Location() != now.Location() {
		t.Errorf("commande programmée : got %v, want 20:00 heure de Paris", got)
	}

	unixScheduled := &models.OrderRequest{EstimatedReady: "1792432800", IsScheduled: true}
	if got := deliveryRequestedAt(unixScheduled, now); got.Equal(now) {
		t.Error("commande programmée (timestamp Unix) : l'heure demandée doit être utilisée")
	}

	unreadable := &models.OrderRequest{EstimatedReady: "2026-10-19 20:00:00", IsScheduled: true}
	if got := deliveryRequestedAt(unreadable, now); !got.Equal(now) {
		t.Error("estimated_ready illisible : repli sur maintenant")
	}
}

func TestNormalizeDeliveryHours(t *testing.T) {
	got, err := normalizeDeliveryHours([]DeliveryHour{
		{DayOfWeek: 3, HourFrom: "19:00:00", HourTo: "22:30"},
		{DayOfWeek: 2, HourFrom: "19:00", HourTo: "00:00"},
		{DayOfWeek: 2, HourFrom: "11:30", HourTo: "14:00"},
	})
	if err != nil {
		t.Fatalf("normalizeDeliveryHours: %v", err)
	}
	want := []DeliveryHour{
		{DayOfWeek: 2, HourFrom: "11:30", HourTo: "14:00"},
		{DayOfWeek: 2, HourFrom: "19:00", HourTo: "00:00"},
		{DayOfWeek: 3, HourFrom: "19:00", HourTo: "22:30"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %+v, want %+v", i, got[i], want[i])
		}
	}

	empty, err := normalizeDeliveryHours(nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("liste vide (suppression de la restriction) : got %+v, %v", empty, err)
	}

	invalid := [][]DeliveryHour{
		{{DayOfWeek: 0, HourFrom: "19:00", HourTo: "22:00"}},
		{{DayOfWeek: 8, HourFrom: "19:00", HourTo: "22:00"}},
		{{DayOfWeek: 1, HourFrom: "25:00", HourTo: "22:00"}},
		{{DayOfWeek: 1, HourFrom: "", HourTo: "22:00"}},
		{{DayOfWeek: 1, HourFrom: "19:00", HourTo: "19:00"}},
	}
	for _, in := range invalid {
		if _, err := normalizeDeliveryHours(in); !errors.Is(err, models.ErrInvalidInput) {
			t.Errorf("%+v : err = %v, want ErrInvalidInput", in, err)
		}
	}
}

func TestWeeklyDeliveryHours(t *testing.T) {
	if weeklyDeliveryHours(nil) != nil {
		t.Error("sans créneau, delivery_hours doit être absent de la réponse")
	}
	weekly := weeklyDeliveryHours(eveningPlusTuesdayLunch())
	if len(weekly) != 7 {
		t.Fatalf("got %d jours, want 7", len(weekly))
	}
	if weekly[1].DayName != "Mardi" || len(weekly[1].Hours) != 2 || len(weekly[0].Hours) != 1 {
		t.Errorf("got %+v", weekly)
	}
}

// Le POS (et les autres canaux) doit rester insensible aux horaires de
// livraison : seul le module scannorder lit delivery_hours. Ce test échoue si
// la table ou la fonction de contrôle apparaît ailleurs dans internal/.
func TestDeliveryHours_OnlyReadByScanNOrder(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	internalDir := filepath.Join(filepath.Dir(file), "..", "..")
	scannorderDir := filepath.Dir(file)

	err := filepath.Walk(internalDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path == scannorderDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, needle := range []string{"delivery_hours", "isDeliveryOpenAt"} {
			if strings.Contains(string(content), needle) {
				t.Errorf("%s référence %q : les horaires de livraison sont propres à ScanNOrder, le POS doit les ignorer", path, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
