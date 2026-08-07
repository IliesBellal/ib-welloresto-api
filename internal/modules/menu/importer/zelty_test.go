package importer

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// Les deux exports reels servant de reference. Ils ont ete produits a onze mois
// d'ecart par la meme enseigne : le premier a un ordre de tags non fiable, le
// second met la categorie en tete. Le parser ne doit dependre ni de l'un ni de
// l'autre.
const (
	fixtureZelty2025 = "Zelty Menu OK PIZZA DLP - 2025-09-24.xlsx"
	fixtureZelty2026 = "Zelty Menu OK Pizza - Devant-les-Ponts - 2026-08-04.xlsx"
)

func parseZeltyFixture(t *testing.T, name string) *IntermediateImport {
	t.Helper()

	got, err := NewZeltyProvider().Parse(openFixture(t, name))
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return got
}

func productByExternalID(t *testing.T, imp *IntermediateImport, externalID string) CanonicalProduct {
	t.Helper()

	for _, p := range imp.Products {
		if p.ExternalID == externalID {
			return p
		}
	}
	t.Fatalf("produit %q absent de l'import", externalID)
	return CanonicalProduct{}
}

func tagByExternalID(t *testing.T, imp *IntermediateImport, externalID string) CanonicalTag {
	t.Helper()

	for _, tag := range imp.Tags {
		if tag.ExternalID == externalID {
			return tag
		}
	}
	t.Fatalf("tag %q absent de l'import", externalID)
	return CanonicalTag{}
}

func attributeByExternalID(t *testing.T, imp *IntermediateImport, externalID string) CanonicalAttribute {
	t.Helper()

	for _, a := range imp.Attributes {
		if a.ExternalID == externalID {
			return a
		}
	}
	t.Fatalf("groupe d'options %q absent de l'import", externalID)
	return CanonicalAttribute{}
}

// Comptes de reference des deux exports, releves sur les fichiers. Ils sont le
// garde-fou de l'automate a sections : une ligne d'en-tete prise pour une
// donnee, une section sautee ou un separateur mal ignore se voit ici.
func TestZeltyParseCounts(t *testing.T) {
	cases := []struct {
		fixture    string
		tags       int
		products   int
		attributes int
		options    int
	}{
		{fixtureZelty2025, 16, 107, 6, 49},
		{fixtureZelty2026, 19, 141, 12, 78},
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			imp := parseZeltyFixture(t, tc.fixture)

			if imp.Provider != ZeltySlug {
				t.Fatalf("Provider = %q, want %q", imp.Provider, ZeltySlug)
			}
			if got := len(imp.Tags); got != tc.tags {
				t.Fatalf("Tags = %d, want %d", got, tc.tags)
			}
			if got := len(imp.Products); got != tc.products {
				t.Fatalf("Products = %d, want %d", got, tc.products)
			}
			if got := len(imp.Attributes); got != tc.attributes {
				t.Fatalf("Attributes = %d, want %d", got, tc.attributes)
			}

			options := 0
			for _, a := range imp.Attributes {
				options += len(a.Options)
			}
			if options != tc.options {
				t.Fatalf("options cumulees = %d, want %d", options, tc.options)
			}
		})
	}
}

// Zelty n'a qu'une notion de libelle : la promotion en categorie caisse est une
// decision de la preview. Le parser doit donc laisser Categories vide et ne
// jamais deviner une categorie, sur les deux formats.
func TestZeltyLeavesCategoriesToThePreview(t *testing.T) {
	for _, fixture := range []string{fixtureZelty2025, fixtureZelty2026} {
		t.Run(fixture, func(t *testing.T) {
			imp := parseZeltyFixture(t, fixture)

			if len(imp.Categories) != 0 {
				t.Fatalf("Categories = %d entrees, want 0 (classification en preview)", len(imp.Categories))
			}
			for _, p := range imp.Products {
				if p.CategoryExternalID != "" {
					t.Fatalf("produit %q: CategoryExternalID = %q, want vide", p.ExternalID, p.CategoryExternalID)
				}
			}
		})
	}
}

// Tous les libelles cites par un produit doivent exister dans Tags : c'est
// l'invariant sur lequel la preview s'appuiera pour proposer une classification
// exhaustive.
func TestZeltyProductTagsAllResolve(t *testing.T) {
	for _, fixture := range []string{fixtureZelty2025, fixtureZelty2026} {
		t.Run(fixture, func(t *testing.T) {
			imp := parseZeltyFixture(t, fixture)

			known := make(map[string]struct{}, len(imp.Tags))
			for _, tag := range imp.Tags {
				known[tag.ExternalID] = struct{}{}
			}
			for _, p := range imp.Products {
				for _, id := range p.TagExternalIDs {
					if _, ok := known[id]; !ok {
						t.Fatalf("produit %q reference le tag inconnu %q", p.ExternalID, id)
					}
				}
			}
		})
	}
}

// Le prix Zelty est unique et s'applique aux trois canaux. La virgule francaise
// doit atterrir en centimes exacts.
func TestZeltyProductPricesAreCentsOnAllChannels(t *testing.T) {
	imp := parseZeltyFixture(t, fixtureZelty2026)

	// "Cree Ta Pizza", 9,9 euros dans le fichier.
	pizza := productByExternalID(t, imp, "ZD1676511")

	if pizza.PriceIn != 990 || pizza.PriceTakeAway != 990 || pizza.PriceDelivery != 990 {
		t.Fatalf("prix = (%d, %d, %d), want (990, 990, 990)", pizza.PriceIn, pizza.PriceTakeAway, pizza.PriceDelivery)
	}
	if pizza.AllPricesZero {
		t.Fatal("AllPricesZero = true pour un produit a 9,90 euros")
	}
}

// Les taux sont conserves bruts. Un 0 sur un canal vaut desactivation, mais
// c'est la preview qui le traduira : ici il doit rester un 0 explicite,
// distinct d'une absence (nil).
func TestZeltyKeepsRawTvaRates(t *testing.T) {
	imp := parseZeltyFixture(t, fixtureZelty2026)

	// "Carbonara" : 10 sur place, 0 en emporte et en livraison.
	carbonara := productByExternalID(t, imp, "ZD1676517")

	cases := []struct {
		channel string
		got     *float64
		want    float64
	}{
		{"sur place", carbonara.TvaRateIn, 10},
		{"emporte", carbonara.TvaRateTakeAway, 0},
		{"livraison", carbonara.TvaRateDelivery, 0},
	}
	for _, tc := range cases {
		if tc.got == nil {
			t.Fatalf("TVA %s = nil, want %v explicite", tc.channel, tc.want)
		}
		if *tc.got != tc.want {
			t.Fatalf("TVA %s = %v, want %v", tc.channel, *tc.got, tc.want)
		}
	}
}

// Les libelles d'un produit sont rendus dans l'ordre du fichier, sans
// interpretation. Le premier est le defaut que la preview proposera comme
// categorie, mais le parser ne le distingue pas des autres.
func TestZeltyProductTagsKeepFileOrder(t *testing.T) {
	imp := parseZeltyFixture(t, fixtureZelty2026)

	// "4 Fromages", tags "NOS PIZZA, VEGE, BASE TOMATE".
	quatreFromages := productByExternalID(t, imp, "ZD1676512")

	wantIDs := []string{"ZT541858", "ZT541863", "ZT541866"}
	wantNames := []string{"NOS PIZZA", "VÉGÉ", "BASE TOMATE"}

	if len(quatreFromages.TagExternalIDs) != len(wantIDs) {
		t.Fatalf("TagExternalIDs = %v, want %v", quatreFromages.TagExternalIDs, wantIDs)
	}
	for i, want := range wantIDs {
		if got := quatreFromages.TagExternalIDs[i]; got != want {
			t.Fatalf("TagExternalIDs[%d] = %q, want %q", i, got, want)
		}
		if got := tagByExternalID(t, imp, want).Name; got != wantNames[i] {
			t.Fatalf("nom du tag %q = %q, want %q", want, got, wantNames[i])
		}
	}
}

// Les lignes de frais n'ont ni prix ni libelle. Elles sont importees quand
// meme, marquees pour le statut removed_from_menu.
func TestZeltyAllPricesZero(t *testing.T) {
	imp2025 := parseZeltyFixture(t, fixtureZelty2025)

	frais := productByExternalID(t, imp2025, "ZD1557688")
	if frais.Name != "Frais de livraison" {
		t.Fatalf("nom = %q, want %q", frais.Name, "Frais de livraison")
	}
	if !frais.AllPricesZero {
		t.Fatal("AllPricesZero = false pour une ligne de frais a 0")
	}
	if frais.PriceIn != 0 || frais.PriceTakeAway != 0 || frais.PriceDelivery != 0 {
		t.Fatalf("prix = (%d, %d, %d), want (0, 0, 0)", frais.PriceIn, frais.PriceTakeAway, frais.PriceDelivery)
	}
	if len(frais.TagExternalIDs) != 0 {
		t.Fatalf("TagExternalIDs = %v, want vide", frais.TagExternalIDs)
	}

	cases := []struct {
		fixture string
		want    int
	}{
		{fixtureZelty2025, 4},
		{fixtureZelty2026, 0},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			imp := parseZeltyFixture(t, tc.fixture)
			zeroed := 0
			for _, p := range imp.Products {
				if p.AllPricesZero {
					zeroed++
				}
			}
			if zeroed != tc.want {
				t.Fatalf("produits sans prix = %d, want %d", zeroed, tc.want)
			}
		})
	}
}

// L'export ne porte aucun min/max sur les groupes d'options : les defauts sont
// poses ici. max_options est NOT NULL sans defaut en base, le laisser a zero
// rendrait le groupe inselectionnable.
func TestZeltyAttributeDefaultsAndOptionPrices(t *testing.T) {
	imp := parseZeltyFixture(t, fixtureZelty2026)

	base := attributeByExternalID(t, imp, "ZO247656")

	if base.Name != "Remplacer la base (optionnel)" {
		t.Fatalf("nom = %q", base.Name)
	}
	if base.Type != AttributeTypeCheck {
		t.Fatalf("Type = %q, want %q", base.Type, AttributeTypeCheck)
	}
	if base.MinOptions != 0 {
		t.Fatalf("MinOptions = %d, want 0", base.MinOptions)
	}
	if base.MaxOptions != len(base.Options) {
		t.Fatalf("MaxOptions = %d, want %d (nombre d'options)", base.MaxOptions, len(base.Options))
	}
	if base.MaxOptions != 6 {
		t.Fatalf("MaxOptions = %d, want 6", base.MaxOptions)
	}
	if base.IsRequired {
		t.Fatal("IsRequired = true, want false")
	}

	// Premiere valeur sans supplement, deuxieme a 0,50 euro.
	wantOptions := []struct {
		externalID string
		title      string
		extraPrice int
	}{
		{"ZOV1251318", "Base Tomate", 0},
		{"ZOV1251319", "Base Crème", 50},
	}
	for i, want := range wantOptions {
		got := base.Options[i]
		if got.ExternalID != want.externalID || got.Title != want.title || got.ExtraPrice != want.extraPrice {
			t.Fatalf("Options[%d] = %+v, want %+v", i, got, want)
		}
	}

	// Un supplement a 2,50 euros dans un autre groupe.
	supplements := attributeByExternalID(t, imp, "ZO247657")
	found := false
	for _, opt := range supplements.Options {
		if opt.ExternalID == "ZOV1251330" {
			found = true
			if opt.ExtraPrice != 250 {
				t.Fatalf("supplement %q = %d, want 250", opt.Title, opt.ExtraPrice)
			}
		}
	}
	if !found {
		t.Fatal("option ZOV1251330 absente")
	}
}

// configurable_attribute_options.title etait en varchar(25), elargi a
// varchar(80) par la migration 081 precisement pour ces libelles. Le parser ne
// doit rien tronquer : la troncature serait invisible et definitive.
func TestZeltyLongOptionTitlesArePreserved(t *testing.T) {
	const legacyTitleLimit = 25

	imp := parseZeltyFixture(t, fixtureZelty2026)

	var long []string
	for _, a := range imp.Attributes {
		for _, opt := range a.Options {
			if utf8.RuneCountInString(opt.Title) > legacyTitleLimit {
				long = append(long, opt.Title)
			}
		}
	}

	want := []string{"Jambon de Parme 24 mois AOP", "Chocolat noisette gianduja"}
	if len(long) != len(want) {
		t.Fatalf("titres de plus de %d caracteres = %v, want %v", legacyTitleLimit, long, want)
	}
	for _, title := range want {
		found := false
		for _, got := range long {
			if got == title {
				found = true
			}
		}
		if !found {
			t.Fatalf("titre %q absent ou tronque, titres longs releves = %v", title, long)
		}
	}
}

// Les identifiants Zelty sont repris tels quels : ce sont eux qui portent
// l'idempotence via import_*_mapping.external_id.
func TestZeltyExternalIDsComeFromTheFile(t *testing.T) {
	imp := parseZeltyFixture(t, fixtureZelty2026)

	type idGroup struct {
		kind   string
		prefix string
		ids    []string
	}

	tagIDs := make([]string, 0, len(imp.Tags))
	for _, tag := range imp.Tags {
		tagIDs = append(tagIDs, tag.ExternalID)
	}
	productIDs := make([]string, 0, len(imp.Products))
	for _, p := range imp.Products {
		productIDs = append(productIDs, p.ExternalID)
	}
	attributeIDs := make([]string, 0, len(imp.Attributes))
	var optionIDs []string
	for _, a := range imp.Attributes {
		attributeIDs = append(attributeIDs, a.ExternalID)
		for _, opt := range a.Options {
			optionIDs = append(optionIDs, opt.ExternalID)
		}
	}

	groups := []idGroup{
		{"tag", "ZT", tagIDs},
		{"produit", "ZD", productIDs},
		{"groupe d'options", "ZO", attributeIDs},
		{"option", "ZOV", optionIDs},
	}

	const maxExternalIDLen = 64
	for _, group := range groups {
		for _, id := range group.ids {
			if !strings.HasPrefix(id, group.prefix) {
				t.Fatalf("%s: identifiant %q, want le prefixe %q", group.kind, id, group.prefix)
			}
			if len(id) > maxExternalIDLen {
				t.Fatalf("%s: identifiant %q de %d caracteres, want <= %d", group.kind, id, len(id), maxExternalIDLen)
			}
		}
	}
}

// Un libelle cite par un produit mais absent de la section Tags ne doit ni
// faire echouer l'import ni disparaitre : il devient un tag synthetique, cree
// au commit comme les autres.
func TestZeltySynthesizesUnknownTagLabels(t *testing.T) {
	imp, err := NewZeltyProvider().Parse(buildXLSX(t, [][]string{
		{},
		{"ID", "Type", "Nom"},
		{"ZT1", "Tag", "NOS PIZZA"},
		{},
		{"ID", "Type", "Nom", "Prix", "TVA", "TVA emporté", "TVA livraison", "Tags"},
		{"ZD1", "Produit", "Margherita", "9,9", "10", "10", "10", "NOS PIZZA, SIGNATURE"},
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(imp.Tags) != 2 {
		t.Fatalf("Tags = %+v, want 2 entrees dont le libelle synthetise", imp.Tags)
	}

	product := imp.Products[0]
	if len(product.TagExternalIDs) != 2 {
		t.Fatalf("TagExternalIDs = %v, want 2 entrees", product.TagExternalIDs)
	}
	if product.TagExternalIDs[0] != "ZT1" {
		t.Fatalf("TagExternalIDs[0] = %q, want %q", product.TagExternalIDs[0], "ZT1")
	}

	synthetic := tagByExternalID(t, imp, product.TagExternalIDs[1])
	if synthetic.Name != "SIGNATURE" {
		t.Fatalf("nom du tag synthetise = %q, want %q", synthetic.Name, "SIGNATURE")
	}
	if !strings.HasPrefix(synthetic.ExternalID, zeltySyntheticTagPrefix+"-") {
		t.Fatalf("identifiant synthetise = %q, want le prefixe %q", synthetic.ExternalID, zeltySyntheticTagPrefix)
	}
}

// Une donnee illisible doit remonter avec sa position, pas etre absorbee : un
// prix silencieusement mis a 0 partirait en base sans que personne ne le voie.
func TestZeltyParseErrors(t *testing.T) {
	cases := []struct {
		name     string
		rows     [][]string
		wantErr  error
		wantText []string
	}{
		{
			name: "prix illisible",
			rows: [][]string{
				{},
				{"ID", "Type", "Nom", "Prix", "TVA", "TVA emporté", "TVA livraison", "Tags"},
				{"ZD1", "Produit", "Margherita", "9,9", "10", "10", "10", ""},
				{"ZD2", "Produit", "Calzone", "offert", "10", "10", "10", ""},
			},
			wantText: []string{"ligne 4", "Prix"},
		},
		{
			name: "taux illisible",
			rows: [][]string{
				{"ID", "Type", "Nom", "Prix", "TVA", "TVA emporté", "TVA livraison", "Tags"},
				{"ZD1", "Produit", "Margherita", "9,9", "dix", "10", "10", ""},
			},
			wantText: []string{"ligne 2", "TVA"},
		},
		{
			name: "valeur d'option orpheline",
			rows: [][]string{
				{"ID", "Type", "Nom", "Prix", "TVA", "TVA emporté", "TVA livraison", "Tags"},
				{"ZD1", "Produit", "Margherita", "9,9", "10", "10", "10", ""},
				{},
				{"ID", "Type", "Nom", "Prix"},
				{"ZOV1", "Option Value", "Base Tomate", "0"},
			},
			wantText: []string{"ligne 5", "Type"},
		},
		{
			name: "produit sans nom",
			rows: [][]string{
				{"ID", "Type", "Nom", "Prix", "TVA", "TVA emporté", "TVA livraison", "Tags"},
				{"ZD1", "Produit", "", "9,9", "10", "10", "10", ""},
			},
			wantText: []string{"ligne 2", "Nom"},
		},
		{
			name: "aucun produit",
			rows: [][]string{
				{"ID", "Type", "Nom"},
				{"ZT1", "Tag", "NOS PIZZA"},
			},
			wantErr: ErrNoProducts,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewZeltyProvider().Parse(buildXLSX(t, tc.rows))
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
