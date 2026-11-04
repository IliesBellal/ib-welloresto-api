package kiosk

import (
	"testing"

	"welloresto-api/internal/models"
)

// Un produit not_available disparaît du menu de la borne, un produit épuisé
// (out_of_stock) reste affiché ; sous-produits d'un groupe compris.
func TestFlattenKioskProducts_HidesNotAvailable(t *testing.T) {
	yes := true
	mk := func(id, status string) models.ProductEntry {
		return models.ProductEntry{ProductID: id, Name: id, Status: status}
	}
	group := mk("g", "available")
	group.IsProductGroup = &yes
	group.SubProducts = []models.ProductEntry{mk("sub-ok", "available"), mk("sub-hidden", "not_available")}

	products := []models.ProductEntry{mk("ok", "available"), mk("hidden", "not_available"), mk("sold", "out_of_stock"), group}
	availability := map[string]bool{"ok": true, "hidden": true, "sold": true, "g": true, "sub-ok": true, "sub-hidden": true}

	var got []string
	for _, p := range flattenKioskProducts(products, availability, nil, models.OrderTypeTakeAway) {
		got = append(got, p.ID)
	}
	want := []string{"ok", "sold", "sub-ok"}
	if len(got) != len(want) {
		t.Fatalf("produits = %v, attendu %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("produits = %v, attendu %v", got, want)
		}
	}
}
