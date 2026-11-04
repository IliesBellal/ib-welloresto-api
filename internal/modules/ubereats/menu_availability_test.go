package ubereats

import (
	"testing"

	"welloresto-api/internal/modules/marketplacecatalog"
)

func TestDiffAvailability(t *testing.T) {
	cat := &marketplacecatalog.Catalog{Products: map[string]*marketplacecatalog.Product{
		"1": {ID: "1", Availability: marketplacecatalog.Available},
		"2": {ID: "2", Availability: marketplacecatalog.SoldOut},
		"3": {ID: "3", Availability: marketplacecatalog.Hidden},
		"4": {ID: "4", Availability: marketplacecatalog.Available},
	}}
	published := []string{"1", "2", "3", "4", "5"} // 5 : retiré du catalogue depuis la publication

	t.Run("rien à envoyer quand tout est aligné", func(t *testing.T) {
		sent := map[string]marketplacecatalog.Availability{
			"1": marketplacecatalog.Available, "2": marketplacecatalog.SoldOut, "3": marketplacecatalog.SoldOut,
			"4": marketplacecatalog.Available, "5": marketplacecatalog.SoldOut,
		}
		if got := diffAvailability(published, cat, sent); len(got) != 0 {
			t.Fatalf("changements = %+v", got)
		}
	})

	t.Run("seule la différence est envoyée", func(t *testing.T) {
		sent := map[string]marketplacecatalog.Availability{
			"1": marketplacecatalog.SoldOut,   // remis en vente
			"2": marketplacecatalog.Available, // épuisé
			"3": marketplacecatalog.Available, // masqué : suspendu en attendant la republication
			"4": marketplacecatalog.Available, // inchangé
			"5": marketplacecatalog.Available, // retiré du catalogue : suspendu
		}
		got := diffAvailability(published, cat, sent)
		want := map[string]marketplacecatalog.Availability{
			"1": marketplacecatalog.Available, "2": marketplacecatalog.SoldOut, "3": marketplacecatalog.SoldOut, "5": marketplacecatalog.SoldOut,
		}
		if len(got) != len(want) {
			t.Fatalf("changements = %+v", got)
		}
		for _, c := range got {
			if want[c.ItemID] != c.Want {
				t.Errorf("article %s : %s, attendu %s", c.ItemID, c.Want, want[c.ItemID])
			}
		}
	})

	t.Run("un article sans état mémorisé est envoyé", func(t *testing.T) {
		got := diffAvailability([]string{"1"}, cat, map[string]marketplacecatalog.Availability{})
		if len(got) != 1 || got[0].Want != marketplacecatalog.Available {
			t.Fatalf("changements = %+v", got)
		}
	})
}
