package menu

import (
	"reflect"
	"testing"

	"welloresto-api/internal/models"
)

func TestSortSubProducts(t *testing.T) {
	intPtr := func(v int) *int { return &v }
	sub := func(id, name string, order *int) models.ProductEntry {
		return models.ProductEntry{ProductID: id, Name: name, DisplayOrder: order}
	}

	products := map[string]*models.ProductEntry{
		"g1": {ProductID: "g1", SubProducts: []models.ProductEntry{
			sub("3", "Sprite", intPtr(2)),
			sub("1", "Fanta", intPtr(1)),
			// display_order absent = 0 : passe avant les ordres positifs,
			// comme côté caisse où le JSON null est lu comme 0.
			sub("4", "Orangina", nil),
			// Ex aequo sur l'ordre : départagés par le nom.
			sub("2", "Coca", intPtr(1)),
		}},
		"solo": {ProductID: "solo"},
	}

	sortSubProducts(products)

	var got []string
	for _, sp := range products["g1"].SubProducts {
		got = append(got, sp.Name)
	}
	want := []string{"Orangina", "Coca", "Fanta", "Sprite"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ordre des sous-produits = %v, want %v", got, want)
	}
}
