package importer

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildManualImport(t *testing.T) {
	tenPercent, reduced := 10.0, 5.5

	imp, err := BuildManualImport([]ManualProduct{
		{
			Name: "Margherita", Description: "Tomate, mozzarella", Category: "Pizzas",
			PriceIn: 990, PriceTakeAway: 990, PriceDelivery: 1150,
			TvaRateIn: &tenPercent, TvaRateTakeAway: &reduced, TvaRateDelivery: &tenPercent,
			Tags: []string{"Signature", "Végétarien"},
		},
		{
			Name: "Calzone", Category: "Pizzas",
			PriceIn: 1290, TvaRateIn: &tenPercent,
			Tags: []string{"Signature"},
		},
		{Name: "Frais de service", Category: "Divers"},
	})
	if err != nil {
		t.Fatalf("BuildManualImport: %v", err)
	}

	if imp.Provider != ManualSlug {
		t.Fatalf("Provider = %q, want %q", imp.Provider, ManualSlug)
	}
	if len(imp.Products) != 3 {
		t.Fatalf("Products = %d, want 3", len(imp.Products))
	}
	if len(imp.Attributes) != 0 {
		t.Fatalf("Attributes = %d, want 0 (la saisie ne porte pas d'options)", len(imp.Attributes))
	}

	margherita := imp.Products[0]
	if margherita.PriceIn != 990 || margherita.PriceDelivery != 1150 {
		t.Fatalf("prix = (%d, %d, %d)", margherita.PriceIn, margherita.PriceTakeAway, margherita.PriceDelivery)
	}
	if margherita.TvaRateTakeAway == nil || *margherita.TvaRateTakeAway != 5.5 {
		t.Fatalf("TvaRateTakeAway = %v, want 5.5", margherita.TvaRateTakeAway)
	}
	if margherita.CategoryExternalID == "" {
		t.Fatal("CategoryExternalID vide alors qu'une catégorie est saisie")
	}
	if len(margherita.TagExternalIDs) != 2 {
		t.Fatalf("TagExternalIDs = %v, want 2", margherita.TagExternalIDs)
	}
	if margherita.AllPricesZero {
		t.Fatal("AllPricesZero = true pour un produit valorisé")
	}

	// Catégories et tags sont dédoublonnés entre lignes.
	if len(imp.Categories) != 2 {
		t.Fatalf("Categories = %+v, want 2 (Pizzas, Divers)", imp.Categories)
	}
	if imp.Products[1].CategoryExternalID != margherita.CategoryExternalID {
		t.Fatal("deux produits de la même catégorie n'ont pas le même identifiant de catégorie")
	}
	if len(imp.Tags) != 2 {
		t.Fatalf("Tags = %+v, want 2", imp.Tags)
	}

	// Une ligne sans aucun prix part en removed_from_menu, comme un fichier.
	if !imp.Products[2].AllPricesZero {
		t.Fatal("AllPricesZero = false pour une ligne sans prix")
	}
	if len(imp.Products[2].TagExternalIDs) != 0 {
		t.Fatalf("TagExternalIDs = %v, want vide", imp.Products[2].TagExternalIDs)
	}
}

// Les identifiants portent l'idempotence : deux saisies du même nom doivent
// produire la même valeur, et le préfixe distingue la porte d'entrée.
func TestBuildManualImportExternalIDsAreStable(t *testing.T) {
	products := []ManualProduct{{Name: "Pizza Margherita", Category: "Pizzas", PriceIn: 990, Tags: []string{"Signature"}}}

	first, err := BuildManualImport(products)
	if err != nil {
		t.Fatalf("BuildManualImport: %v", err)
	}
	second, err := BuildManualImport(products)
	if err != nil {
		t.Fatalf("BuildManualImport: %v", err)
	}

	if first.Products[0].ExternalID != second.Products[0].ExternalID {
		t.Fatalf("identifiant instable: %q != %q", first.Products[0].ExternalID, second.Products[0].ExternalID)
	}

	prefixes := []struct {
		kind string
		id   string
		want string
	}{
		{"produit", first.Products[0].ExternalID, manualProductPrefix},
		{"catégorie", first.Categories[0].ExternalID, manualCategoryPrefix},
		{"tag", first.Tags[0].ExternalID, manualTagPrefix},
	}
	for _, tc := range prefixes {
		if !strings.HasPrefix(tc.id, tc.want+"-") {
			t.Fatalf("identifiant %s = %q, want le préfixe %q", tc.kind, tc.id, tc.want)
		}
	}
}

func TestBuildManualImportErrors(t *testing.T) {
	negative := -1.0

	cases := []struct {
		name     string
		products []ManualProduct
		wantErr  error
		wantText []string
	}{
		{
			name:     "aucun produit",
			products: nil,
			wantErr:  ErrNoProducts,
		},
		{
			name:     "produit sans nom",
			products: []ManualProduct{{Name: "  ", Category: "Pizzas"}},
			wantText: []string{"ligne 1", "name"},
		},
		{
			name: "nom dupliqué",
			products: []ManualProduct{
				{Name: "Margherita", Category: "Pizzas"},
				{Name: "Calzone", Category: "Pizzas"},
				{Name: " margherita ", Category: "Pizzas"},
			},
			wantText: []string{"ligne 3", "ligne 1"},
		},
		{
			name:     "taux négatif",
			products: []ManualProduct{{Name: "Margherita", TvaRateDelivery: &negative}},
			wantText: []string{"ligne 1", "tva_delivery"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildManualImport(tc.products)
			if err == nil {
				t.Fatal("BuildManualImport = nil, want une erreur")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("erreur = %v, want %v", err, tc.wantErr)
			}
			for _, fragment := range tc.wantText {
				if !strings.Contains(err.Error(), fragment) {
					t.Fatalf("message = %q, want qu'il contienne %q", err.Error(), fragment)
				}
			}
		})
	}
}
