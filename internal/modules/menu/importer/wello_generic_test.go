package importer

import (
	"errors"
	"strings"
	"testing"
)

// templateHeader est la ligne d'en-tete du template tel qu'il sera genere.
var templateHeader = []string{
	"Nom", "Description", "Catégorie",
	"Prix sur place", "Prix emporté", "Prix livraison",
	"TVA sur place", "TVA emporté", "TVA livraison",
	"Tags",
}

func parseWelloGeneric(t *testing.T, rows [][]string) *IntermediateImport {
	t.Helper()

	imp, err := NewWelloGenericProvider().Parse(buildXLSX(t, rows))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return imp
}

func TestWelloGenericParse(t *testing.T) {
	imp := parseWelloGeneric(t, [][]string{
		templateHeader,
		{"Margherita", "Tomate, mozzarella, basilic", "Pizzas", "9,90", "9,90", "11,50", "10", "5,5", "10", "Végétarien, Signature"},
		{"Calzone", "", "Pizzas", "12,90", "12,90", "14", "10", "5,5", "10", ""},
		{"Tiramisu", "Maison", "Desserts", "6", "6", "6", "5,5", "5,5", "5,5", "Signature"},
	})

	if imp.Provider != WelloGenericSlug {
		t.Fatalf("Provider = %q, want %q", imp.Provider, WelloGenericSlug)
	}
	if len(imp.Products) != 3 {
		t.Fatalf("Products = %d, want 3", len(imp.Products))
	}
	if len(imp.Attributes) != 0 {
		t.Fatalf("Attributes = %d, want 0 (le template ne porte pas d'options)", len(imp.Attributes))
	}

	margherita := imp.Products[0]
	if margherita.Name != "Margherita" {
		t.Fatalf("Name = %q", margherita.Name)
	}
	if margherita.Description != "Tomate, mozzarella, basilic" {
		t.Fatalf("Description = %q", margherita.Description)
	}
	if margherita.PriceIn != 990 || margherita.PriceTakeAway != 990 || margherita.PriceDelivery != 1150 {
		t.Fatalf("prix = (%d, %d, %d), want (990, 990, 1150)",
			margherita.PriceIn, margherita.PriceTakeAway, margherita.PriceDelivery)
	}
	if margherita.AllPricesZero {
		t.Fatal("AllPricesZero = true pour un produit valorise")
	}

	rates := []struct {
		channel string
		got     *float64
		want    float64
	}{
		{"sur place", margherita.TvaRateIn, 10},
		{"emporte", margherita.TvaRateTakeAway, 5.5},
		{"livraison", margherita.TvaRateDelivery, 10},
	}
	for _, tc := range rates {
		if tc.got == nil {
			t.Fatalf("TVA %s = nil, want %v", tc.channel, tc.want)
		}
		if *tc.got != tc.want {
			t.Fatalf("TVA %s = %v, want %v", tc.channel, *tc.got, tc.want)
		}
	}

	// Le template designe explicitement la categorie : contrairement a Zelty,
	// le produit sort du parse avec sa categorie deja resolue.
	if margherita.CategoryExternalID == "" {
		t.Fatal("CategoryExternalID vide alors que la colonne Categorie est renseignee")
	}
	if len(imp.Categories) != 2 {
		t.Fatalf("Categories = %+v, want 2 (Pizzas et Desserts dedoublonnes)", imp.Categories)
	}
	if imp.Categories[0].Name != "Pizzas" || imp.Categories[1].Name != "Desserts" {
		t.Fatalf("Categories = %+v, want [Pizzas Desserts] dans l'ordre du fichier", imp.Categories)
	}
	if imp.Products[1].CategoryExternalID != margherita.CategoryExternalID {
		t.Fatal("deux produits de la meme categorie n'ont pas le meme CategoryExternalID")
	}

	// Les tags sont dedoublonnes globalement mais gardent leur ordre par ligne.
	if len(imp.Tags) != 2 {
		t.Fatalf("Tags = %+v, want 2", imp.Tags)
	}
	if len(margherita.TagExternalIDs) != 2 {
		t.Fatalf("TagExternalIDs = %v, want 2", margherita.TagExternalIDs)
	}
	if len(imp.Products[1].TagExternalIDs) != 0 {
		t.Fatalf("produit sans tag: TagExternalIDs = %v, want vide", imp.Products[1].TagExternalIDs)
	}
	if got, want := imp.Products[2].TagExternalIDs, margherita.TagExternalIDs[1:]; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("tag partage = %v, want %v", got, want)
	}
}

// Le template n'a pas d'identifiant source : l'identite d'une ligne est son
// nom. Deux imports successifs du meme fichier doivent produire les memes
// identifiants, sinon la cle d'idempotence ne sert a rien.
func TestWelloGenericExternalIDsAreStable(t *testing.T) {
	rows := [][]string{
		templateHeader,
		{"Pizza Margherita", "", "Pizzas", "9,90", "", "", "10", "", "", "Signature"},
	}

	first := parseWelloGeneric(t, rows)
	second := parseWelloGeneric(t, rows)

	if first.Products[0].ExternalID != second.Products[0].ExternalID {
		t.Fatalf("identifiant produit instable: %q != %q", first.Products[0].ExternalID, second.Products[0].ExternalID)
	}
	if first.Categories[0].ExternalID != second.Categories[0].ExternalID {
		t.Fatalf("identifiant categorie instable: %q != %q", first.Categories[0].ExternalID, second.Categories[0].ExternalID)
	}
	if first.Tags[0].ExternalID != second.Tags[0].ExternalID {
		t.Fatalf("identifiant tag instable: %q != %q", first.Tags[0].ExternalID, second.Tags[0].ExternalID)
	}

	prefixes := []struct {
		kind string
		id   string
		want string
	}{
		{"produit", first.Products[0].ExternalID, welloGenericProductPrefix},
		{"categorie", first.Categories[0].ExternalID, welloGenericCategoryPrefix},
		{"tag", first.Tags[0].ExternalID, welloGenericTagPrefix},
	}
	for _, tc := range prefixes {
		if !strings.HasPrefix(tc.id, tc.want+"-") {
			t.Fatalf("identifiant %s = %q, want le prefixe %q", tc.kind, tc.id, tc.want)
		}
	}
}

// Les colonnes facultatives absentes laissent le champ a sa valeur neutre : la
// preview completera. Une ligne entierement a zero est marquee, comme chez
// Zelty, pour partir en removed_from_menu.
func TestWelloGenericOptionalColumnsAndZeroPrices(t *testing.T) {
	imp := parseWelloGeneric(t, [][]string{
		{"Nom", "Catégorie", "Prix sur place"},
		{"Margherita", "Pizzas", "9,90"},
		{"Frais de livraison", "Divers", "0"},
	})

	margherita := imp.Products[0]
	if margherita.PriceTakeAway != 0 || margherita.PriceDelivery != 0 {
		t.Fatalf("prix des colonnes absentes = (%d, %d), want (0, 0)", margherita.PriceTakeAway, margherita.PriceDelivery)
	}
	if margherita.TvaRateIn != nil || margherita.TvaRateTakeAway != nil || margherita.TvaRateDelivery != nil {
		t.Fatal("taux des colonnes absentes non nil, want nil (absence, pas zero)")
	}
	if margherita.Description != "" {
		t.Fatalf("Description = %q, want vide", margherita.Description)
	}
	if margherita.AllPricesZero {
		t.Fatal("AllPricesZero = true alors que le prix sur place est renseigne")
	}

	if !imp.Products[1].AllPricesZero {
		t.Fatal("AllPricesZero = false pour une ligne entierement a 0")
	}
}

// Une cellule categorie vide n'est pas rejetee : la categorie est obligatoire a
// la creation, mais c'est la preview qui la reclame, produit sous les yeux.
func TestWelloGenericAcceptsEmptyCategoryCell(t *testing.T) {
	imp := parseWelloGeneric(t, [][]string{
		{"Nom", "Catégorie", "Prix sur place"},
		{"Margherita", "", "9,90"},
	})

	if got := imp.Products[0].CategoryExternalID; got != "" {
		t.Fatalf("CategoryExternalID = %q, want vide", got)
	}
	if len(imp.Categories) != 0 {
		t.Fatalf("Categories = %+v, want vide", imp.Categories)
	}
}

// Les en-tetes sont reconnus a la casse, aux espaces et aux accents pres : le
// restaurateur retape parfois la ligne, ou son tableur la reformate.
func TestWelloGenericHeaderIsAccentAndCaseInsensitive(t *testing.T) {
	imp := parseWelloGeneric(t, [][]string{
		{"NOM", "  categorie ", "Prix sur place", "PRIX  EMPORTE", "TVA sur place"},
		{"Margherita", "Pizzas", "9,90", "10,50", "10"},
	})

	product := imp.Products[0]
	if product.Name != "Margherita" || product.PriceIn != 990 || product.PriceTakeAway != 1050 {
		t.Fatalf("produit = %+v", product)
	}
	if len(imp.Categories) != 1 || imp.Categories[0].Name != "Pizzas" {
		t.Fatalf("Categories = %+v", imp.Categories)
	}
}

// La ligne d'en-tete n'est pas forcement la premiere du classeur : un export
// retravaille peut commencer par des lignes vides.
func TestWelloGenericSkipsLeadingBlankRows(t *testing.T) {
	imp := parseWelloGeneric(t, [][]string{
		{},
		{"", "", ""},
		{"Nom", "Catégorie", "Prix sur place"},
		{"Margherita", "Pizzas", "9,90"},
		{},
		{"Calzone", "Pizzas", "12,90"},
	})

	if len(imp.Products) != 2 {
		t.Fatalf("Products = %d, want 2", len(imp.Products))
	}
}

func TestWelloGenericParseErrors(t *testing.T) {
	cases := []struct {
		name     string
		rows     [][]string
		wantErr  error
		wantText []string
	}{
		{
			name: "colonne obligatoire absente",
			rows: [][]string{
				{"Nom", "Prix sur place"},
				{"Margherita", "9,90"},
			},
			wantErr:  ErrMissingColumn,
			wantText: []string{"Categorie"},
		},
		{
			name: "nom duplique",
			rows: [][]string{
				{"Nom", "Catégorie", "Prix sur place"},
				{"Margherita", "Pizzas", "9,90"},
				{"Calzone", "Pizzas", "12,90"},
				{"  margherita ", "Pizzas", "10,90"},
			},
			wantText: []string{"ligne 4", "ligne 2", "Nom"},
		},
		{
			name: "ligne renseignee sans nom",
			rows: [][]string{
				{"Nom", "Catégorie", "Prix sur place"},
				{"", "Pizzas", "9,90"},
			},
			wantText: []string{"ligne 2", "Nom"},
		},
		{
			name: "prix illisible",
			rows: [][]string{
				{"Nom", "Catégorie", "Prix sur place"},
				{"Margherita", "Pizzas", "neuf euros"},
			},
			wantText: []string{"ligne 2", "Prix sur place"},
		},
		{
			name: "taux illisible",
			rows: [][]string{
				{"Nom", "Catégorie", "Prix sur place", "TVA livraison"},
				{"Margherita", "Pizzas", "9,90", "dix"},
			},
			wantText: []string{"ligne 2", "TVA livraison"},
		},
		{
			name: "aucun produit",
			rows: [][]string{
				{"Nom", "Catégorie", "Prix sur place"},
			},
			wantErr: ErrNoProducts,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWelloGenericProvider().Parse(buildXLSX(t, tc.rows))
			if err == nil {
				t.Fatal("Parse = nil, want une erreur")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("Parse = %v, want %v", err, tc.wantErr)
			}
			for _, fragment := range tc.wantText {
				if !strings.Contains(err.Error(), fragment) {
					t.Fatalf("message = %q, want qu'il contienne %q", err.Error(), fragment)
				}
			}
		})
	}
}
