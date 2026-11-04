package ubereats

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"welloresto-api/internal/modules/marketplacecatalog"
	"welloresto-api/internal/modules/openinghours"
)

func TestServiceAvailabilityFromSlots(t *testing.T) {
	t.Run("plage de jours, mêmes heures", func(t *testing.T) {
		got := ServiceAvailabilityFromSlots([]openinghours.Slot{{DayOfWeekFrom: 1, DayOfWeekTo: 3, HourFrom: "11:30:00", HourTo: "14:00:00"}})
		if len(got) != 3 || got[0].DayOfWeek != "monday" || got[2].DayOfWeek != "wednesday" {
			t.Fatalf("jours = %+v", got)
		}
		if got[1].TimePeriods[0] != (TimePeriod{"11:30", "14:00"}) {
			t.Fatalf("plage = %+v", got[1].TimePeriods)
		}
	})

	t.Run("service du soir qui passe minuit est coupé en deux jours", func(t *testing.T) {
		got := ServiceAvailabilityFromSlots([]openinghours.Slot{{DayOfWeekFrom: 5, DayOfWeekTo: 5, HourFrom: "19:00:00", HourTo: "02:00:00"}})
		if len(got) != 2 {
			t.Fatalf("jours = %+v", got)
		}
		if got[0].DayOfWeek != "friday" || got[0].TimePeriods[0] != (TimePeriod{"19:00", "23:59"}) {
			t.Fatalf("vendredi = %+v", got[0])
		}
		if got[1].DayOfWeek != "saturday" || got[1].TimePeriods[0] != (TimePeriod{"00:00", "02:00"}) {
			t.Fatalf("samedi = %+v", got[1])
		}
	})

	t.Run("dimanche soir passe minuit : report sur le lundi", func(t *testing.T) {
		got := ServiceAvailabilityFromSlots([]openinghours.Slot{{DayOfWeekFrom: 7, DayOfWeekTo: 7, HourFrom: "20:00", HourTo: "01:00"}})
		if len(got) != 2 || got[0].DayOfWeek != "monday" || got[1].DayOfWeek != "sunday" {
			t.Fatalf("jours = %+v", got)
		}
	})

	t.Run("fermeture à 00:00 = fin de journée", func(t *testing.T) {
		got := ServiceAvailabilityFromSlots([]openinghours.Slot{{DayOfWeekFrom: 2, DayOfWeekTo: 2, HourFrom: "18:00:00", HourTo: "00:00:00"}})
		if len(got) != 1 || got[0].TimePeriods[0] != (TimePeriod{"18:00", "23:59"}) {
			t.Fatalf("got = %+v", got)
		}
	})

	t.Run("ouvert toute la journée", func(t *testing.T) {
		got := ServiceAvailabilityFromSlots([]openinghours.Slot{{DayOfWeekFrom: 1, DayOfWeekTo: 1, HourFrom: "00:00:00", HourTo: "00:00:00"}})
		if len(got) != 1 || got[0].TimePeriods[0] != (TimePeriod{"00:00", "23:59"}) {
			t.Fatalf("got = %+v", got)
		}
	})

	t.Run("plages qui se chevauchent fusionnées, triées", func(t *testing.T) {
		got := ServiceAvailabilityFromSlots([]openinghours.Slot{
			{DayOfWeekFrom: 1, DayOfWeekTo: 1, HourFrom: "18:00", HourTo: "22:00"},
			{DayOfWeekFrom: 1, DayOfWeekTo: 1, HourFrom: "11:00", HourTo: "14:00"},
			{DayOfWeekFrom: 1, DayOfWeekTo: 1, HourFrom: "13:30", HourTo: "15:00"},
		})
		want := []TimePeriod{{"11:00", "15:00"}, {"18:00", "22:00"}}
		if len(got) != 1 || len(got[0].TimePeriods) != 2 || got[0].TimePeriods[0] != want[0] || got[0].TimePeriods[1] != want[1] {
			t.Fatalf("got = %+v", got)
		}
	})

	t.Run("créneaux invalides ignorés", func(t *testing.T) {
		got := ServiceAvailabilityFromSlots([]openinghours.Slot{
			{DayOfWeekFrom: 0, DayOfWeekTo: 3, HourFrom: "11:00", HourTo: "14:00"},
			{DayOfWeekFrom: 1, DayOfWeekTo: 1, HourFrom: "abc", HourTo: "14:00"},
			{DayOfWeekFrom: 1, DayOfWeekTo: 1, HourFrom: "12:00", HourTo: "12:00"},
		})
		if got != nil {
			t.Fatalf("got = %+v", got)
		}
	})
}

func sampleCatalog() *marketplacecatalog.Catalog {
	return &marketplacecatalog.Catalog{
		Categories: []marketplacecatalog.Category{
			{ID: "10", Name: "Pizzas", ProductIDs: []string{"1", "2", "3"}},
			{ID: "11", Name: "Desserts", ProductIDs: []string{"4"}},
		},
		Products: map[string]*marketplacecatalog.Product{
			"1": {ID: "1", Name: "4 fromages", Description: "Mozzarella", PriceCents: 1290, TaxRate: 10, Availability: marketplacecatalog.Available,
				OptionGroups: []marketplacecatalog.OptionGroup{{ID: "g1", Title: "Base", Min: 1, Max: 1, Options: []marketplacecatalog.Option{{ID: "o1", Title: "Tomate", PriceCents: 0}, {ID: "o2", Title: "Crème", PriceCents: 100}}}}},
			"2": {ID: "2", Name: "Bière", PriceCents: 450, TaxRate: 20, ContainsAlcohol: true, Availability: marketplacecatalog.SoldOut},
			"3": {ID: "3", Name: "Masquée", PriceCents: 900, TaxRate: 10, Availability: marketplacecatalog.Hidden},
			"4": {ID: "4", Name: "Tiramisu", PriceCents: 30000, TaxRate: 10, Availability: marketplacecatalog.Available},
		},
	}
}

var testHours = []ServiceAvailability{{DayOfWeek: "monday", TimePeriods: []TimePeriod{{"11:00", "22:00"}}}}

func TestBuildMenu(t *testing.T) {
	b, err := BuildMenu(sampleCatalog(), "Chez Luigi", testHours)
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(b.ProductItemIDs, ","); got != "1,2" {
		t.Fatalf("produits publiés = %s (le masqué et le produit trop cher sont exclus)", got)
	}
	if b.States["1"] != marketplacecatalog.Available || b.States["2"] != marketplacecatalog.SoldOut {
		t.Fatalf("états = %+v", b.States)
	}
	if len(b.Menu.Categories) != 1 || b.Menu.Categories[0].ID != "cat-10" {
		t.Fatalf("catégories = %+v (la catégorie vide est écartée)", b.Menu.Categories)
	}
	if len(b.Warnings) != 1 || !strings.Contains(b.Warnings[0], "Tiramisu") {
		t.Fatalf("avertissements = %v", b.Warnings)
	}

	// Options : article « opt_ », groupe « mg_ », correspondances pour l'import.
	if b.OptionItems["opt_o2"] != "o2" || b.GroupItems["mg_g1"] != "g1" {
		t.Fatalf("correspondances = %+v / %+v", b.OptionItems, b.GroupItems)
	}

	items := map[string]UberItem{}
	for _, it := range b.Menu.Items {
		items[it.ID] = it
	}
	if items["opt_o2"].PriceInfo.Price != 100 || items["opt_o2"].TaxInfo.VATRatePercentage != 10 {
		t.Fatalf("option = %+v", items["opt_o2"])
	}
	if items["1"].TaxInfo.VATRatePercentage != 10 || items["2"].TaxInfo.VATRatePercentage != 20 {
		t.Fatal("la TVA publiée doit être la vraie TVA de livraison du produit")
	}
	if items["1"].SuspensionInfo != nil {
		t.Fatal("un produit disponible n'a pas de suspension")
	}
	if s := items["2"].SuspensionInfo; s == nil || s.Suspension.SuspendUntil != suspendForever {
		t.Fatalf("suspension du produit épuisé = %+v", s)
	}
	if items["2"].DishInfo == nil || items["2"].DishInfo.Classifications.AlcoholicItems != 1 {
		t.Fatal("la classification alcool doit être envoyée")
	}
	if items["1"].DishInfo != nil {
		t.Fatal("pas de classification pour un produit sans alcool")
	}

	// Le document est du JSON conforme : tableaux jamais null, horaires présents.
	raw, err := json.Marshal(b.Menu)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{`"modifier_groups":[`, `"vat_rate_percentage":10`, `"service_availability":[{"day_of_week":"monday"`, `"suspend_until":8640000000`} {
		if !strings.Contains(s, want) {
			t.Errorf("le document ne contient pas %s", want)
		}
	}
	if strings.Contains(s, "core_price") || strings.Contains(s, "media_info") {
		t.Error("champs hors schéma Uber présents")
	}
}

func TestBuildMenu_FingerprintIgnoresStock(t *testing.T) {
	a, err := BuildMenu(sampleCatalog(), "X", testHours)
	if err != nil {
		t.Fatal(err)
	}

	cat := sampleCatalog()
	cat.Products["1"].Availability = marketplacecatalog.SoldOut // rupture : pas de renvoi du menu
	b, err := BuildMenu(cat, "X", testHours)
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint != b.Fingerprint {
		t.Fatal("une rupture de stock ne doit pas changer l'empreinte du menu")
	}

	cat = sampleCatalog()
	cat.Products["2"].Availability = marketplacecatalog.Hidden // retrait : le menu change
	c, err := BuildMenu(cat, "X", testHours)
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint == c.Fingerprint {
		t.Fatal("masquer un produit doit changer l'empreinte (le menu doit être republié)")
	}

	d, err := BuildMenu(sampleCatalog(), "X", []ServiceAvailability{{DayOfWeek: "tuesday", TimePeriods: []TimePeriod{{"11:00", "22:00"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint == d.Fingerprint {
		t.Fatal("un changement d'horaires doit changer l'empreinte")
	}
}

func TestBuildMenu_Errors(t *testing.T) {
	if _, err := BuildMenu(sampleCatalog(), "X", nil); !errors.Is(err, ErrNoOpeningHours) {
		t.Fatalf("sans horaires: %v", err)
	}
	cat := sampleCatalog()
	for _, p := range cat.Products {
		p.Availability = marketplacecatalog.Hidden
	}
	if _, err := BuildMenu(cat, "X", testHours); !errors.Is(err, ErrNothingPublishable) {
		t.Fatalf("tout masqué: %v", err)
	}
}
