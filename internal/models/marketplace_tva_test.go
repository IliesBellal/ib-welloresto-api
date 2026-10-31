package models

import "testing"

func TestMarketplaceTVAFor(t *testing.T) {
	if got := MarketplaceTVAFor(false); got != (MarketplaceTVA{In: 5, TakeAway: 2, Delivery: 7}) {
		t.Fatalf("standard = %+v", got)
	}
	if got := MarketplaceTVAFor(true); got != (MarketplaceTVA{In: 6, TakeAway: 3, Delivery: 9}) {
		t.Fatalf("alcool = %+v", got)
	}
}

func TestLooksAlcoholic(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"🍺 Heineken (33cl)", true},
		{"Bière blonde", true},
		{"Biere Pression 25cl", true},
		{"Vin rouge – bouteille", true},
		{"Rosé de Provence (vin)", true},
		{"🍹 Spritz Aperol", true},
		{"Kronenbourg 1664", true},
		{"Desperados", true},
		{"Bière sans alcool", false},
		{"Heineken 0.0", false},
		{"Leffe 0,0", false},
		{"🍕 Pizza Margherita", false},
		{"🥤Coca-Cola (33cl) 🔴", false},
		{"Vinaigrette maison", false},
		{"Évian (50cl)", false},
		{"Cuisse de poulet", false},
		{"Pâtes à la crème de ginger", false},
		{"", false},
	}
	for _, c := range cases {
		if got := LooksAlcoholic(c.name); got != c.want {
			t.Errorf("LooksAlcoholic(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}
