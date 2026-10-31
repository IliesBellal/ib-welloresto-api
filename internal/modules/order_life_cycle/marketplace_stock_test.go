package order_life_cycle

import (
	"testing"

	"welloresto-api/internal/models"
)

func strp(s string) *string { return &s }

func TestStockWarningComment(t *testing.T) {
	products := []models.OrderProductPayload{
		{ProductID: "10", ProductName: "Pizza Fromage"},
		{ProductID: "11", ProductName: "  "},
		{ProductID: "12", ProductName: "Oasis"},
	}
	cases := []struct {
		name     string
		existing *string
		ids      []string
		want     string
	}{
		{"un produit, pas de commentaire", nil, []string{"10"},
			"Rupture de stock : Pizza Fromage (à vérifier avant d'accepter)"},
		{"le texte du client suit", strp("  Sans oignons "), []string{"10"},
			"Rupture de stock : Pizza Fromage (à vérifier avant d'accepter)\nSans oignons"},
		{"plusieurs produits", nil, []string{"10", "12"},
			"Rupture de stock : Pizza Fromage, Oasis (à vérifier avant d'accepter)"},
		{"nom absent : identifiant", nil, []string{"11"},
			"Rupture de stock : produit 11 (à vérifier avant d'accepter)"},
		{"produit inconnu de la commande", nil, []string{"99"},
			"Rupture de stock : produit 99 (à vérifier avant d'accepter)"},
		{"doublon", nil, []string{"10", "10"},
			"Rupture de stock : Pizza Fromage (à vérifier avant d'accepter)"},
		{"commentaire existant blanc", strp("   "), []string{"12"},
			"Rupture de stock : Oasis (à vérifier avant d'accepter)"},
	}
	for _, c := range cases {
		got := stockWarningComment(c.existing, products, c.ids)
		if got == nil || *got != c.want {
			t.Errorf("%s : %v, want %q", c.name, got, c.want)
		}
	}
}

func TestIsMarketplaceBrand(t *testing.T) {
	for brand, want := range map[string]bool{
		models.BrandUberEats: true, models.BrandDeliveroo: true,
		models.BrandWelloResto: false, "": false, "KIOSK": false,
	} {
		if got := models.IsMarketplaceBrand(brand); got != want {
			t.Errorf("IsMarketplaceBrand(%q) = %v, want %v", brand, got, want)
		}
	}
}
